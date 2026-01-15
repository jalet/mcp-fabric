package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	"github.com/jarsater/mcp-fabric/operator/internal/metrics"
)

const (
	// Default values for Task limits
	defaultMaxIterations         = int32(100)
	defaultIterationTimeout      = 30 * time.Minute
	defaultTotalTimeout          = 24 * time.Hour
	defaultMaxConsecutiveFailures = int32(3)
	defaultCompletionSignal      = "<promise>COMPLETE</promise>"
	defaultCheckInterval         = 5 * time.Second

	// Requeue intervals
	iterationRequeueDelay = 5 * time.Second
	failureRequeueDelay   = 30 * time.Second
)

// TaskReconciler reconciles a Task object.
type TaskReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	HTTPClient *http.Client
}

// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

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
		// Don't requeue while paused
		return ctrl.Result{}, nil
	}

	// Check if task is already completed or failed
	if task.Status.Phase == aiv1alpha1.TaskPhaseCompleted ||
		task.Status.Phase == aiv1alpha1.TaskPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Check limits
	limits := r.getEffectiveLimits(&task)

	// Check max iterations
	if task.Status.CurrentIteration >= *limits.MaxIterations {
		task.Status.Phase = aiv1alpha1.TaskPhaseFailed
		task.Status.Message = fmt.Sprintf("Max iterations reached: %d", *limits.MaxIterations)
		now := metav1.Now()
		task.Status.CompletedAt = &now
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "MaxIterationsReached",
			Message:            task.Status.Message,
		})
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Check total timeout
	if task.Status.StartedAt != nil {
		elapsed := time.Since(task.Status.StartedAt.Time)
		if elapsed > limits.TotalTimeout.Duration {
			task.Status.Phase = aiv1alpha1.TaskPhaseFailed
			task.Status.Message = fmt.Sprintf("Total timeout exceeded: %v", limits.TotalTimeout.Duration)
			now := metav1.Now()
			task.Status.CompletedAt = &now
			r.setCondition(&task, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				ObservedGeneration: task.Generation,
				Reason:             "TotalTimeoutExceeded",
				Message:            task.Status.Message,
			})
			if err := r.Status().Update(ctx, &task); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Check consecutive failures
	if task.Status.ConsecutiveFailures >= *limits.MaxConsecutiveFailures {
		task.Status.Phase = aiv1alpha1.TaskPhasePaused
		task.Status.Message = fmt.Sprintf("Paused after %d consecutive failures", task.Status.ConsecutiveFailures)
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "ConsecutiveFailures",
			Message:            task.Status.Message,
		})
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Verify orchestrator agent is ready
	orchestrator, err := r.getAgent(ctx, task.Spec.OrchestratorRef, task.Namespace)
	if err != nil {
		logger.Error(err, "Failed to get orchestrator agent")
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "OrchestratorNotFound",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	if !orchestrator.Status.Ready {
		logger.Info("Orchestrator agent not ready", "agent", orchestrator.Name)
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "OrchestratorNotReady",
			Message:            "Waiting for orchestrator agent to be ready",
		})
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Load PRD content
	prdContent, err := r.loadTaskSource(ctx, &task)
	if err != nil {
		logger.Error(err, "Failed to load task source")
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "TaskSourceError",
			Message:            err.Error(),
		})
		if err := r.Status().Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: failureRequeueDelay}, nil
	}

	// Load progress if configured
	progressContent := ""
	if task.Spec.ProgressTracking != nil && task.Spec.ProgressTracking.Type == aiv1alpha1.ProgressTrackingTypeConfigMap {
		progressContent, _ = r.loadProgress(ctx, &task) // Ignore errors, progress may not exist yet
	}

	// Mark as running if pending
	if task.Status.Phase == aiv1alpha1.TaskPhasePending {
		task.Status.Phase = aiv1alpha1.TaskPhaseRunning
		now := metav1.Now()
		task.Status.StartedAt = &now
	}

	// Execute iteration
	iterationResult := r.executeIteration(ctx, &task, orchestrator, prdContent, progressContent, limits)

	// Update iteration status
	task.Status.CurrentIteration++
	now := metav1.Now()
	task.Status.LastIterationAt = &now

	// Add to recent iterations (keep last 10)
	task.Status.RecentIterations = append(task.Status.RecentIterations, iterationResult)
	if len(task.Status.RecentIterations) > 10 {
		task.Status.RecentIterations = task.Status.RecentIterations[1:]
	}

	if iterationResult.Passed {
		task.Status.ConsecutiveFailures = 0
		task.Status.CompletedTasks++
		task.Status.LastTaskID = iterationResult.TaskID
	} else {
		task.Status.ConsecutiveFailures++
	}

	// Check for completion signal
	completionSignal := defaultCompletionSignal
	if task.Spec.Completion != nil && task.Spec.Completion.Signal != "" {
		completionSignal = task.Spec.Completion.Signal
	}

	if strings.Contains(iterationResult.Learnings, completionSignal) {
		task.Status.Phase = aiv1alpha1.TaskPhaseCompleted
		task.Status.Message = "All tasks completed successfully"
		task.Status.CompletedAt = &now
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: task.Generation,
			Reason:             "Completed",
			Message:            "All tasks completed successfully",
		})
	} else {
		r.setCondition(&task, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: task.Generation,
			Reason:             "InProgress",
			Message:            fmt.Sprintf("Iteration %d completed, continuing", task.Status.CurrentIteration),
		})
	}

	// Update progress if tracking is enabled
	if iterationResult.Learnings != "" && task.Spec.ProgressTracking != nil {
		if err := r.appendProgress(ctx, &task, iterationResult); err != nil {
			logger.Error(err, "Failed to update progress")
		}
	}

	task.Status.ObservedGeneration = task.Generation
	if err := r.Status().Update(ctx, &task); err != nil {
		metrics.RecordReconcile(metrics.ControllerTask, metrics.ResultError, time.Since(startTime).Seconds())
		return ctrl.Result{}, err
	}

	// Record metrics
	metrics.SetTaskMetrics(
		task.Name,
		task.Namespace,
		string(task.Status.Phase),
		int(task.Status.CurrentIteration),
		int(task.Status.CompletedTasks),
		int(task.Status.TotalTasks),
	)
	metrics.RecordReconcile(metrics.ControllerTask, metrics.ResultSuccess, time.Since(startTime).Seconds())

	// Requeue for next iteration if still running
	if task.Status.Phase == aiv1alpha1.TaskPhaseRunning {
		return ctrl.Result{RequeueAfter: iterationRequeueDelay}, nil
	}

	return ctrl.Result{}, nil
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

// loadProgress loads existing progress content.
func (r *TaskReconciler) loadProgress(ctx context.Context, task *aiv1alpha1.Task) (string, error) {
	if task.Spec.ProgressTracking == nil || task.Spec.ProgressTracking.ConfigMapRef == nil {
		return "", nil
	}

	var cm corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{
		Name:      task.Spec.ProgressTracking.ConfigMapRef.Name,
		Namespace: task.Namespace,
	}, &cm)

	if errors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	key := task.Spec.ProgressTracking.ConfigMapRef.Key
	if key == "" {
		key = "progress.txt"
	}

	return cm.Data[key], nil
}

// appendProgress appends iteration results to the progress tracking.
func (r *TaskReconciler) appendProgress(ctx context.Context, task *aiv1alpha1.Task, result aiv1alpha1.IterationResult) error {
	if task.Spec.ProgressTracking == nil || task.Spec.ProgressTracking.Type != aiv1alpha1.ProgressTrackingTypeConfigMap {
		return nil
	}

	if task.Spec.ProgressTracking.ConfigMapRef == nil {
		return nil
	}

	cmName := task.Spec.ProgressTracking.ConfigMapRef.Name
	key := task.Spec.ProgressTracking.ConfigMapRef.Key
	if key == "" {
		key = "progress.txt"
	}

	// Get or create ConfigMap
	var cm corev1.ConfigMap
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: task.Namespace}, &cm)

	if errors.IsNotFound(err) {
		// Create new ConfigMap
		cm = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: task.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "mcp-fabric",
					"fabric.jarsater.ai/task":      task.Name,
				},
			},
			Data: make(map[string]string),
		}
	} else if err != nil {
		return err
	}

	// Build progress entry
	status := "FAILED"
	if result.Passed {
		status = "PASSED"
	}

	entry := fmt.Sprintf(`
---

## Iteration %d - %s
**Task:** %s - %s
**Status:** %s
**Learnings:**
%s
`,
		result.Iteration,
		result.StartedAt.Format(time.RFC3339),
		result.TaskID,
		result.TaskTitle,
		status,
		result.Learnings,
	)

	// Append to existing content
	existing := cm.Data[key]
	cm.Data[key] = existing + entry

	// Create or update
	if cm.CreationTimestamp.IsZero() {
		return r.Create(ctx, &cm)
	}
	return r.Update(ctx, &cm)
}

// executeIteration runs a single iteration of the task loop.
func (r *TaskReconciler) executeIteration(
	ctx context.Context,
	task *aiv1alpha1.Task,
	orchestrator *aiv1alpha1.Agent,
	prdContent string,
	progressContent string,
	limits *aiv1alpha1.TaskLimits,
) aiv1alpha1.IterationResult {
	logger := log.FromContext(ctx)
	startTime := metav1.Now()

	result := aiv1alpha1.IterationResult{
		Iteration: task.Status.CurrentIteration + 1,
		StartedAt: startTime,
		Passed:    false,
	}

	// Build the query for the orchestrator
	query := buildOrchestratorQuery(task, prdContent, progressContent)

	// Set timeout for this iteration
	iterationCtx, cancel := context.WithTimeout(ctx, limits.IterationTimeout.Duration)
	defer cancel()

	// Call the orchestrator agent
	response, err := r.invokeAgent(iterationCtx, orchestrator, query, task)
	if err != nil {
		logger.Error(err, "Failed to invoke orchestrator agent")
		result.Error = err.Error()
		now := metav1.Now()
		result.CompletedAt = &now
		return result
	}

	// Parse response
	parseOrchestratorResponse(response, &result)

	now := metav1.Now()
	result.CompletedAt = &now

	logger.Info("Iteration completed",
		"iteration", result.Iteration,
		"taskId", result.TaskID,
		"passed", result.Passed,
	)

	return result
}

// invokeAgent calls an agent via HTTP.
func (r *TaskReconciler) invokeAgent(ctx context.Context, agent *aiv1alpha1.Agent, query string, task *aiv1alpha1.Task) (map[string]interface{}, error) {
	endpoint := agent.Status.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("agent %s has no endpoint", agent.Name)
	}

	// Build request
	reqBody := map[string]interface{}{
		"query": query,
		"metadata": map[string]interface{}{
			"taskName":      task.Name,
			"taskNamespace": task.Namespace,
			"iteration":     task.Status.CurrentIteration + 1,
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	url := fmt.Sprintf("http://%s/invoke", endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute
	httpClient := r.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Return raw response wrapped
		return map[string]interface{}{"raw": string(respBody)}, nil
	}

	return result, nil
}

// buildOrchestratorQuery constructs the prompt for the orchestrator agent.
func buildOrchestratorQuery(task *aiv1alpha1.Task, prdContent string, progressContent string) string {
	var sb strings.Builder

	sb.WriteString("You are orchestrating an autonomous task execution loop.\n\n")

	sb.WriteString("## Current State\n")
	sb.WriteString(fmt.Sprintf("- Task: %s\n", task.Name))
	sb.WriteString(fmt.Sprintf("- Iteration: %d\n", task.Status.CurrentIteration+1))
	sb.WriteString(fmt.Sprintf("- Completed Tasks: %d/%d\n", task.Status.CompletedTasks, task.Status.TotalTasks))
	sb.WriteString(fmt.Sprintf("- Consecutive Failures: %d\n", task.Status.ConsecutiveFailures))
	sb.WriteString("\n")

	if task.Spec.Context != "" {
		sb.WriteString("## Additional Context\n")
		sb.WriteString(task.Spec.Context)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## PRD (Task List)\n```json\n")
	sb.WriteString(prdContent)
	sb.WriteString("\n```\n\n")

	if progressContent != "" {
		sb.WriteString("## Previous Progress\n")
		sb.WriteString(progressContent)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Instructions\n")
	sb.WriteString("1. Identify the highest-priority incomplete task (passes=false)\n")
	sb.WriteString("2. Execute or delegate the task to the worker agent\n")
	sb.WriteString("3. Report the result in structured format\n")
	sb.WriteString("4. If ALL tasks are complete, include: <promise>COMPLETE</promise>\n\n")

	sb.WriteString("## Expected Response Format\n")
	sb.WriteString("```json\n")
	sb.WriteString(`{
  "taskId": "story-XXX",
  "taskTitle": "Task title",
  "passed": true/false,
  "learnings": "What was learned or changed",
  "error": "Error message if failed"
}`)
	sb.WriteString("\n```\n")

	return sb.String()
}

// parseOrchestratorResponse extracts structured data from the agent response.
func parseOrchestratorResponse(response map[string]interface{}, result *aiv1alpha1.IterationResult) {
	// Try to extract structured fields
	if taskID, ok := response["taskId"].(string); ok {
		result.TaskID = taskID
	}
	if taskTitle, ok := response["taskTitle"].(string); ok {
		result.TaskTitle = taskTitle
	}
	if passed, ok := response["passed"].(bool); ok {
		result.Passed = passed
	}
	if learnings, ok := response["learnings"].(string); ok {
		result.Learnings = learnings
	}
	if errMsg, ok := response["error"].(string); ok {
		result.Error = errMsg
	}

	// Check for nested result field (common in gateway responses)
	if nested, ok := response["result"].(map[string]interface{}); ok {
		if taskID, ok := nested["taskId"].(string); ok {
			result.TaskID = taskID
		}
		if taskTitle, ok := nested["taskTitle"].(string); ok {
			result.TaskTitle = taskTitle
		}
		if passed, ok := nested["passed"].(bool); ok {
			result.Passed = passed
		}
		if learnings, ok := nested["learnings"].(string); ok {
			result.Learnings = learnings
		}
		if errMsg, ok := nested["error"].(string); ok {
			result.Error = errMsg
		}
	}

	// Check for raw response (text output from agent)
	if raw, ok := response["raw"].(string); ok {
		if result.Learnings == "" {
			result.Learnings = raw
		}
		// Check for completion signal in raw output
		if strings.Contains(raw, "<promise>COMPLETE</promise>") {
			result.Passed = true
		}
	}
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
		Named("task").
		Complete(r)
}
