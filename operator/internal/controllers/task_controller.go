package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	"github.com/jarsater/mcp-fabric/operator/internal/metrics"
	"github.com/jarsater/mcp-fabric/operator/internal/render"
)

const (
	// Default values for Task limits
	defaultMaxIterations          = int32(100)
	defaultIterationTimeout       = 30 * time.Minute
	defaultTotalTimeout           = 24 * time.Hour
	defaultMaxConsecutiveFailures = int32(3)

	// Default orchestrator agent name
	defaultOrchestratorName = "task-orchestrator"

	// Requeue intervals
	jobPollInterval     = 10 * time.Second
	failureRequeueDelay = 30 * time.Second

	// Marker for orchestrator result in logs
	orchestratorResultMarker = "ORCHESTRATOR_RESULT:"

	// Finalizer for Task cleanup
	taskFinalizer = "fabric.jarsater.ai/task-cleanup"

	// Maximum Job recreations before failing
	maxJobRecreations = 3
	jobRecreationAnnotation = "fabric.jarsater.ai/job-recreations"
)

// TaskReconciler reconciles a Task object.
type TaskReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Clientset *kubernetes.Clientset
}

// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/log,verbs=get

// Reconcile handles Task reconciliation.
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	// Fetch the Task
	var task aiv1alpha1.Task
	if err := r.Get(ctx, req.NamespacedName, &task); err != nil {
		if client.IgnoreNotFound(err) == nil {
			metrics.DeleteTaskMetrics(req.Name, req.Namespace)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling Task", "name", task.Name, "phase", task.Status.Phase)

	// Handle deletion with finalizer
	if !task.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &task)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&task, taskFinalizer) {
		controllerutil.AddFinalizer(&task, taskFinalizer)
		if err := r.Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if needed
	if task.Status.Phase == "" {
		task.Status.Phase = aiv1alpha1.TaskPhasePending
		task.Status.CurrentIteration = 0
		task.Status.CompletedTasks = 0
		task.Status.ConsecutiveFailures = 0
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if task is paused
	if task.Spec.Paused {
		if task.Status.Phase != aiv1alpha1.TaskPhasePaused {
			task.Status.Phase = aiv1alpha1.TaskPhasePaused
			r.setCondition(&task, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: task.Generation,
				Reason:             "Paused",
				Message:            "Task is paused",
			})
			if err := r.Status().Update(ctx, &task); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if task is already completed or failed
	if task.Status.Phase == aiv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == aiv1alpha1.TaskPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Handle based on phase
	var result ctrl.Result
	var err error

	switch task.Status.Phase {
	case aiv1alpha1.TaskPhasePending:
		result, err = r.handlePendingPhase(ctx, &task)
	case aiv1alpha1.TaskPhaseRunning:
		result, err = r.handleRunningPhase(ctx, &task)
	default:
		// Re-evaluate paused tasks
		if task.Status.Phase == aiv1alpha1.TaskPhasePaused && !task.Spec.Paused {
			task.Status.Phase = aiv1alpha1.TaskPhaseRunning
			task.Status.ConsecutiveFailures = 0
			r.setCondition(&task, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: task.Generation,
				Reason:             "Resumed",
				Message:            "Task resumed from paused state",
			})
			if err := r.Status().Update(ctx, &task); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// Record metrics - track both success and error cases
	metrics.SetTaskMetrics(
		task.Name,
		task.Namespace,
		string(task.Status.Phase),
		int(task.Status.CurrentIteration),
		int(task.Status.CompletedTasks),
		int(task.Status.TotalTasks),
	)
	if err != nil {
		metrics.RecordReconcile(metrics.ControllerTask, metrics.ResultError, time.Since(startTime).Seconds())
	} else {
		metrics.RecordReconcile(metrics.ControllerTask, metrics.ResultSuccess, time.Since(startTime).Seconds())
	}

	return result, err
}

// handlePendingPhase sets up the task and launches the orchestrator Job.
func (r *TaskReconciler) handlePendingPhase(ctx context.Context, task *aiv1alpha1.Task) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling pending phase", "task", task.Name)

	// Get orchestrator agent
	orchestratorAgent, err := r.getOrchestratorAgent(ctx, task)
	if err != nil {
		logger.Error(err, "Failed to get orchestrator agent")
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "OrchestratorNotFound",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Get worker agent (needed for endpoint)
	workerAgent, err := r.getAgent(ctx, task.Spec.WorkerRef, task.Namespace)
	if err != nil {
		logger.Error(err, "Failed to get worker agent")
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "WorkerNotFound",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Ensure workspace PVC exists
	if err := r.reconcileWorkspacePVC(ctx, task); err != nil {
		logger.Error(err, "Failed to reconcile workspace PVC")
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, err
	}

	// Load PRD content
	prdContent, err := r.loadTaskSource(ctx, task)
	if err != nil {
		logger.Error(err, "Failed to load task source")
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "TaskSourceError",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Count total tasks in PRD
	totalTasks := r.countTasksInPRD(prdContent)

	// Build worker endpoint from agent spec
	workerEndpoint := r.buildWorkerEndpoint(workerAgent)

	// Create orchestrator Job
	jobParams := render.OrchestratorJobParams{
		Task:              task,
		OrchestratorAgent: orchestratorAgent,
		WorkerEndpoint:    workerEndpoint,
		WorkspacePVC:      render.WorkspacePVCName(task),
		PRD:               prdContent,
	}

	job, err := render.OrchestratorJob(jobParams)
	if err != nil {
		logger.Error(err, "Failed to render orchestrator Job")
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "JobRenderError",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Set controller reference
	if err := ctrl.SetControllerReference(task, job, r.Scheme); err != nil {
		logger.Error(err, "Failed to set controller reference on Job")
		return ctrl.Result{}, err
	}

	// Create the Job
	if err := r.Create(ctx, job); err != nil {
		if !errors.IsAlreadyExists(err) {
			logger.Error(err, "Failed to create orchestrator Job")
			return ctrl.Result{RequeueAfter: failureRequeueDelay}, err
		}
		logger.Info("Orchestrator Job already exists", "job", job.Name)
	} else {
		logger.Info("Created orchestrator Job", "job", job.Name)
	}

	// Update status to Running
	now := metav1.Now()
	task.Status.Phase = aiv1alpha1.TaskPhaseRunning
	task.Status.StartedAt = &now
	task.Status.TotalTasks = int32(totalTasks)
	if task.Spec.Git != nil {
		task.Status.RepositoryURL = task.Spec.Git.URL
	}
	r.setCondition(task, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: task.Generation,
		Reason:             "Running",
		Message:            "Orchestrator Job started",
	})

	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: jobPollInterval}, nil
}

// handleRunningPhase monitors the orchestrator Job and extracts results.
func (r *TaskReconciler) handleRunningPhase(ctx context.Context, task *aiv1alpha1.Task) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check total timeout
	limits := r.getEffectiveLimits(task)
	if task.Status.StartedAt != nil {
		elapsed := time.Since(task.Status.StartedAt.Time)
		if elapsed > limits.TotalTimeout.Duration {
			task.Status.Phase = aiv1alpha1.TaskPhaseFailed
			task.Status.Message = fmt.Sprintf("Total timeout exceeded: %v", limits.TotalTimeout.Duration)
			now := metav1.Now()
			task.Status.CompletedAt = &now
			r.setCondition(task, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: task.Generation,
				Reason:             "TotalTimeoutExceeded",
				Message:            task.Status.Message,
			})
			if err := r.Status().Update(ctx, task); err != nil {
				return ctrl.Result{}, err
			}
			// Cleanup the Job
			r.cleanupOrchestratorJob(ctx, task)
			return ctrl.Result{}, nil
		}
	}

	// Get orchestrator Job
	jobName := fmt.Sprintf("%s-orchestrator", task.Name)
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: task.Namespace}, &job); err != nil {
		if errors.IsNotFound(err) {
			// Track recreation count to prevent infinite loops
			recreations := 0
			if task.Annotations != nil {
				if v, ok := task.Annotations[jobRecreationAnnotation]; ok {
					fmt.Sscanf(v, "%d", &recreations)
				}
			}
			recreations++

			maxRecreations := int32(maxJobRecreations)
			if task.Spec.Limits != nil && task.Spec.Limits.MaxJobRecreations != nil {
				maxRecreations = *task.Spec.Limits.MaxJobRecreations
			}

			if int32(recreations) > maxRecreations {
				logger.Info("Max Job recreations exceeded, failing task", "job", jobName, "recreations", recreations)
				task.Status.Phase = aiv1alpha1.TaskPhaseFailed
				task.Status.Message = fmt.Sprintf("Orchestrator Job lost %d times, giving up", recreations-1)
				now := metav1.Now()
				task.Status.CompletedAt = &now
				if err := r.Status().Update(ctx, task); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			}

			logger.Info("Orchestrator Job not found, recreating", "job", jobName, "recreation", recreations)
			if task.Annotations == nil {
				task.Annotations = map[string]string{}
			}
			task.Annotations[jobRecreationAnnotation] = fmt.Sprintf("%d", recreations)
			if err := r.Update(ctx, task); err != nil {
				return ctrl.Result{}, err
			}
			task.Status.Phase = aiv1alpha1.TaskPhasePending
			if err := r.Status().Update(ctx, task); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
		}
		return ctrl.Result{}, err
	}

	// Check Job status
	if job.Status.Succeeded > 0 {
		logger.Info("Orchestrator Job succeeded", "job", jobName)
		return r.handleJobSuccess(ctx, task, &job)
	}

	if job.Status.Failed > 0 {
		logger.Info("Orchestrator Job failed", "job", jobName)
		return r.handleJobFailure(ctx, task, &job)
	}

	// Check for deadline exceeded
	for _, cond := range job.Status.Conditions {
		if cond.Type == batchv1.JobFailed && cond.Reason == "DeadlineExceeded" {
			logger.Info("Orchestrator Job deadline exceeded", "job", jobName)
			task.Status.Phase = aiv1alpha1.TaskPhaseFailed
			task.Status.Message = "Orchestrator Job deadline exceeded"
			now := metav1.Now()
			task.Status.CompletedAt = &now
			r.setCondition(task, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: task.Generation,
				Reason:             "JobDeadlineExceeded",
				Message:            task.Status.Message,
			})
			if err := r.Status().Update(ctx, task); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Job still running, requeue to check again
	logger.V(1).Info("Orchestrator Job still running", "job", jobName)
	return ctrl.Result{RequeueAfter: jobPollInterval}, nil
}

// OrchestratorResult represents the result from the orchestrator Job.
type OrchestratorResult struct {
	Passed         bool            `json:"passed"`
	CompletedTasks int             `json:"completedTasks"`
	TotalTasks     int             `json:"totalTasks"`
	Iterations     int             `json:"iterations"`
	Learnings      string          `json:"learnings"`
	CommitSHA      string          `json:"commitSha"`
	PullRequestURL string          `json:"pullRequestUrl"`
	PRD            json.RawMessage `json:"prd"`
	Error          string          `json:"error"`
	NoChanges      bool            `json:"noChanges"`
	Pushed         bool            `json:"pushed"`
	GitError       string          `json:"gitError"`
}

// handleJobSuccess processes a successful orchestrator Job.
func (r *TaskReconciler) handleJobSuccess(ctx context.Context, task *aiv1alpha1.Task, job *batchv1.Job) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Extract result from Job logs
	result, err := r.getOrchestratorResult(ctx, job)
	if err != nil {
		logger.Error(err, "Failed to get orchestrator result from logs")
		// Job succeeded but couldn't extract result - treat as success
		result = &OrchestratorResult{Passed: true, Learnings: "Job completed but result extraction failed"}
	}

	// Update task status
	now := metav1.Now()
	task.Status.CompletedAt = &now
	task.Status.CurrentIteration = int32(result.Iterations)
	task.Status.CompletedTasks = int32(result.CompletedTasks)
	if result.TotalTasks > 0 {
		task.Status.TotalTasks = int32(result.TotalTasks)
	}

	if result.Passed {
		task.Status.Phase = aiv1alpha1.TaskPhaseCompleted
		task.Status.Message = "All tasks completed successfully"
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: task.Generation,
			Reason:             "Completed",
			Message:            task.Status.Message,
		})
	} else {
		task.Status.Phase = aiv1alpha1.TaskPhaseFailed
		task.Status.Message = "Orchestrator completed but not all tasks passed"
		if result.Error != "" {
			task.Status.Message = result.Error
		}
		r.setCondition(task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "PartialCompletion",
			Message:            task.Status.Message,
		})
	}

	// Update git status fields
	if result.CommitSHA != "" {
		task.Status.LastCommitSHA = result.CommitSHA
	}
	if result.PullRequestURL != "" {
		task.Status.PullRequestURL = result.PullRequestURL
	}

	// Add final iteration result
	iterResult := aiv1alpha1.IterationResult{
		Iteration:   int32(result.Iterations),
		Passed:      result.Passed,
		CompletedAt: &now,
		Learnings:   result.Learnings,
	}
	// Guard against nil StartedAt pointer
	if task.Status.StartedAt != nil {
		iterResult.StartedAt = *task.Status.StartedAt
	} else {
		iterResult.StartedAt = now
	}
	task.Status.RecentIterations = append(task.Status.RecentIterations, iterResult)
	if len(task.Status.RecentIterations) > 10 {
		task.Status.RecentIterations = task.Status.RecentIterations[len(task.Status.RecentIterations)-10:]
	}

	task.Status.ObservedGeneration = task.Generation

	// Update the PRD in source ConfigMap if provided
	if len(result.PRD) > 0 {
		if err := r.persistUpdatedPRD(ctx, task, string(result.PRD)); err != nil {
			logger.Error(err, "Failed to persist updated PRD")
		}
	}

	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Task completed",
		"passed", result.Passed,
		"completedTasks", result.CompletedTasks,
		"totalTasks", result.TotalTasks,
		"prUrl", result.PullRequestURL,
	)

	return ctrl.Result{}, nil
}

// handleJobFailure processes a failed orchestrator Job.
func (r *TaskReconciler) handleJobFailure(ctx context.Context, task *aiv1alpha1.Task, job *batchv1.Job) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Try to extract any result from logs
	result, _ := r.getOrchestratorResult(ctx, job)

	now := metav1.Now()
	task.Status.Phase = aiv1alpha1.TaskPhaseFailed
	task.Status.CompletedAt = &now
	task.Status.Message = "Orchestrator Job failed"

	if result != nil {
		task.Status.CurrentIteration = int32(result.Iterations)
		task.Status.CompletedTasks = int32(result.CompletedTasks)
		if result.Error != "" {
			task.Status.Message = result.Error
		}
		if result.CommitSHA != "" {
			task.Status.LastCommitSHA = result.CommitSHA
		}
	}

	r.setCondition(task, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		ObservedGeneration: task.Generation,
		Reason:             "JobFailed",
		Message:            task.Status.Message,
	})

	task.Status.ObservedGeneration = task.Generation
	if err := r.Status().Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Task failed", "message", task.Status.Message)
	return ctrl.Result{}, nil
}

// getOrchestratorResult extracts the result from orchestrator Job logs.
func (r *TaskReconciler) getOrchestratorResult(ctx context.Context, job *batchv1.Job) (*OrchestratorResult, error) {
	if r.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset not available")
	}

	// Find the pod for this Job
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(job.Namespace), client.MatchingLabels{
		"job-name": job.Name,
	}); err != nil {
		return nil, fmt.Errorf("failed to list Job pods: %w", err)
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for Job %s", job.Name)
	}

	// Get logs from the orchestrator container
	pod := podList.Items[0]
	tailLines := int64(1000)
	req := r.Clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: "orchestrator",
		TailLines: &tailLines,
	})

	logs, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod logs: %w", err)
	}
	defer logs.Close()

	// Scan line-by-line and track the last line containing the result marker.
	var resultLine string
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, orchestratorResultMarker); idx != -1 {
			resultLine = line[idx+len(orchestratorResultMarker):]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read pod logs: %w", err)
	}

	if resultLine == "" {
		return nil, fmt.Errorf("orchestrator result marker not found in logs")
	}

	var result OrchestratorResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(resultLine)), &result); err != nil {
		return nil, fmt.Errorf("failed to parse orchestrator result: %w", err)
	}

	return &result, nil
}

// cleanupOrchestratorJob deletes the orchestrator Job.
func (r *TaskReconciler) cleanupOrchestratorJob(ctx context.Context, task *aiv1alpha1.Task) {
	jobName := fmt.Sprintf("%s-orchestrator", task.Name)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: task.Namespace,
		},
	}
	_ = r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground))
}

// handleDeletion handles Task deletion by cleaning up owned resources.
func (r *TaskReconciler) handleDeletion(ctx context.Context, task *aiv1alpha1.Task) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(task, taskFinalizer) {
		// Finalizer already removed, nothing to do
		return ctrl.Result{}, nil
	}

	logger.Info("Handling Task deletion, cleaning up resources", "task", task.Name)

	// Clean up orchestrator Job
	r.cleanupOrchestratorJob(ctx, task)

	// Clean up workspace PVC
	pvcName := render.WorkspacePVCName(task)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: task.Namespace,
		},
	}
	if err := r.Delete(ctx, pvc); err != nil && !errors.IsNotFound(err) {
		logger.Error(err, "Failed to delete workspace PVC", "pvc", pvcName)
		// Continue with cleanup even if PVC deletion fails
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(task, taskFinalizer)
	if err := r.Update(ctx, task); err != nil {
		return ctrl.Result{}, err
	}

	// Clean up metrics
	metrics.DeleteTaskMetrics(task.Name, task.Namespace)

	logger.Info("Task cleanup completed", "task", task.Name)
	return ctrl.Result{}, nil
}

// ==============================================================================
// Helper Functions
// ==============================================================================

// getOrchestratorAgent gets the orchestrator agent for the task.
func (r *TaskReconciler) getOrchestratorAgent(ctx context.Context, task *aiv1alpha1.Task) (*aiv1alpha1.Agent, error) {
	ref := task.Spec.OrchestratorRef
	if ref == nil {
		// Use default orchestrator
		ref = &aiv1alpha1.AgentReference{
			Name:      defaultOrchestratorName,
			Namespace: task.Namespace,
		}
	}
	return r.getAgent(ctx, *ref, task.Namespace)
}

// getAgent retrieves an Agent by reference.
func (r *TaskReconciler) getAgent(ctx context.Context, ref aiv1alpha1.AgentReference, defaultNS string) (*aiv1alpha1.Agent, error) {
	ns := ref.Namespace
	if ns == "" {
		ns = defaultNS
	}

	var agent aiv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &agent); err != nil {
		return nil, err
	}

	return &agent, nil
}

// buildWorkerEndpoint builds the worker HTTP endpoint from agent spec.
func (r *TaskReconciler) buildWorkerEndpoint(agent *aiv1alpha1.Agent) string {
	// Build endpoint from agent name and namespace
	// Format: http://{name}.{namespace}:8080
	// Using default port 8080 as agents expose this port
	return fmt.Sprintf("http://%s.%s:8080", agent.Name, agent.Namespace)
}

// PRDDocument represents the structure of a PRD JSON document.
type PRDDocument struct {
	Tasks []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"tasks"`
	// Alternative structure: some PRDs may use "stories" instead of "tasks"
	Stories []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"stories"`
}

// countTasksInPRD counts the total number of tasks in the PRD using proper JSON parsing.
func (r *TaskReconciler) countTasksInPRD(prdContent string) int {
	var prd PRDDocument
	if err := json.Unmarshal([]byte(prdContent), &prd); err != nil {
		// If JSON parsing fails, return 0 (unknown task count)
		return 0
	}

	// Check both "tasks" and "stories" fields for flexibility
	taskCount := len(prd.Tasks)
	if taskCount == 0 {
		taskCount = len(prd.Stories)
	}

	return taskCount
}

// getEffectiveLimits returns the limits with defaults applied.
func (r *TaskReconciler) getEffectiveLimits(task *aiv1alpha1.Task) *aiv1alpha1.TaskLimits {
	limits := &aiv1alpha1.TaskLimits{}

	if task.Spec.Limits != nil {
		limits = task.Spec.Limits.DeepCopy()
	}

	if limits.MaxIterations == nil {
		maxIter := defaultMaxIterations
		limits.MaxIterations = &maxIter
	}

	if limits.IterationTimeout == nil {
		limits.IterationTimeout = &metav1.Duration{Duration: defaultIterationTimeout}
	}

	if limits.TotalTimeout == nil {
		limits.TotalTimeout = &metav1.Duration{Duration: defaultTotalTimeout}
	}

	if limits.MaxConsecutiveFailures == nil {
		maxFail := defaultMaxConsecutiveFailures
		limits.MaxConsecutiveFailures = &maxFail
	}

	return limits
}

// reconcileWorkspacePVC ensures the workspace PVC exists.
func (r *TaskReconciler) reconcileWorkspacePVC(ctx context.Context, task *aiv1alpha1.Task) error {
	pvc := render.TaskWorkspacePVC(task)

	// Set controller reference
	if err := ctrl.SetControllerReference(task, pvc, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Check if PVC exists
	var existingPVC corev1.PersistentVolumeClaim
	err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, &existingPVC)

	if errors.IsNotFound(err) {
		// Create PVC
		if err := r.Create(ctx, pvc); err != nil {
			return fmt.Errorf("failed to create workspace PVC: %w", err)
		}
		return nil
	}

	return err
}

// loadTaskSource loads the PRD content from the configured source.
func (r *TaskReconciler) loadTaskSource(ctx context.Context, task *aiv1alpha1.Task) (string, error) {
	source := task.Spec.TaskSource

	switch source.Type {
	case aiv1alpha1.TaskSourceTypeInline:
		return source.Inline, nil

	case aiv1alpha1.TaskSourceTypeConfigMap:
		if source.ConfigMapRef == nil {
			return "", fmt.Errorf("configMapRef is required for configmap source type")
		}
		var cm corev1.ConfigMap
		if err := r.Get(ctx, types.NamespacedName{
			Name:      source.ConfigMapRef.Name,
			Namespace: task.Namespace,
		}, &cm); err != nil {
			return "", fmt.Errorf("failed to get ConfigMap %s: %w", source.ConfigMapRef.Name, err)
		}
		key := source.ConfigMapRef.Key
		if key == "" {
			key = "prd.json"
		}
		content, ok := cm.Data[key]
		if !ok {
			return "", fmt.Errorf("key %s not found in ConfigMap %s", key, source.ConfigMapRef.Name)
		}
		return content, nil

	case aiv1alpha1.TaskSourceTypeSecret:
		if source.SecretRef == nil {
			return "", fmt.Errorf("secretRef is required for secret source type")
		}
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{
			Name:      source.SecretRef.Name,
			Namespace: task.Namespace,
		}, &secret); err != nil {
			return "", fmt.Errorf("failed to get Secret %s: %w", source.SecretRef.Name, err)
		}
		key := source.SecretRef.Key
		if key == "" {
			key = "prd.json"
		}
		content, ok := secret.Data[key]
		if !ok {
			return "", fmt.Errorf("key %s not found in Secret %s", key, source.SecretRef.Name)
		}
		return string(content), nil

	default:
		return "", fmt.Errorf("unknown task source type: %s", source.Type)
	}
}

// persistUpdatedPRD writes the updated PRD back to the source ConfigMap.
func (r *TaskReconciler) persistUpdatedPRD(ctx context.Context, task *aiv1alpha1.Task, updatedPRD string) error {
	source := task.Spec.TaskSource

	// Only persist to ConfigMap sources
	if source.Type != aiv1alpha1.TaskSourceTypeConfigMap {
		return nil
	}

	if source.ConfigMapRef == nil {
		return fmt.Errorf("configMapRef is required for configmap source type")
	}

	cmName := source.ConfigMapRef.Name
	key := source.ConfigMapRef.Key
	if key == "" {
		key = "prd.json"
	}

	// Get the ConfigMap
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: task.Namespace}, &cm); err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", cmName, err)
	}

	// Update the PRD content
	cm.Data[key] = updatedPRD

	// Update the ConfigMap
	if err := r.Update(ctx, &cm); err != nil {
		return fmt.Errorf("failed to update ConfigMap %s: %w", cmName, err)
	}

	return nil
}

func (r *TaskReconciler) setCondition(task *aiv1alpha1.Task, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&task.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.Task{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Named("task").
		Complete(r)
}
