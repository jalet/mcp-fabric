package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// parseOrchestratorResultFromLogs extracts the orchestrator result from log content.
// This is a helper for testing that mirrors the parsing logic in getOrchestratorResult.
func parseOrchestratorResultFromLogs(logStr string) (*OrchestratorResult, error) {
	idx := strings.LastIndex(logStr, orchestratorResultMarker)
	if idx == -1 {
		return nil, fmt.Errorf("orchestrator result marker not found in logs")
	}

	jsonStart := idx + len(orchestratorResultMarker)
	jsonStr := logStr[jsonStart:]

	if newlineIdx := strings.Index(jsonStr, "\n"); newlineIdx != -1 {
		jsonStr = jsonStr[:newlineIdx]
	}

	var result OrchestratorResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse orchestrator result: %w", err)
	}

	return &result, nil
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = aiv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)
	return scheme
}

func newTestReconciler(objs ...client.Object) *TaskReconciler {
	scheme := newTestScheme()
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&aiv1alpha1.Task{}).
		Build()

	return &TaskReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}
}

func TestReconcile_NotFound(t *testing.T) {
	r := newTestReconciler()
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for not found")
	}
}

func TestReconcile_InitializesStatus(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-task",
			Namespace:  "default",
			Finalizers: []string{taskFinalizer}, // Include finalizer so we test status init
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
		// Status.Phase is empty - should initialize
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after status initialization")
	}

	// Verify status was updated
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}
	if updatedTask.Status.Phase != aiv1alpha1.TaskPhasePending {
		t.Errorf("expected phase Pending, got %s", updatedTask.Status.Phase)
	}
}

func TestReconcile_PausedTask(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-task",
			Namespace:  "default",
			Finalizers: []string{taskFinalizer},
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
			Paused: true,
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhaseRunning,
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue {
		t.Error("expected no requeue for paused task")
	}

	// Verify task transitioned to Paused
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}
	if updatedTask.Status.Phase != aiv1alpha1.TaskPhasePaused {
		t.Errorf("expected phase Paused, got %s", updatedTask.Status.Phase)
	}
}

func TestReconcile_CompletedTaskNoOp(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-task",
			Namespace:  "default",
			Finalizers: []string{taskFinalizer},
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhaseCompleted,
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Error("expected no requeue for completed task")
	}
}

func TestGetEffectiveLimits_Defaults(t *testing.T) {
	r := newTestReconciler()
	task := &aiv1alpha1.Task{
		Spec: aiv1alpha1.TaskSpec{
			Limits: nil, // No limits specified
		},
	}

	limits := r.getEffectiveLimits(task)

	if *limits.MaxIterations != defaultMaxIterations {
		t.Errorf("expected MaxIterations %d, got %d", defaultMaxIterations, *limits.MaxIterations)
	}
	if limits.IterationTimeout.Duration != defaultIterationTimeout {
		t.Errorf("expected IterationTimeout %v, got %v", defaultIterationTimeout, limits.IterationTimeout.Duration)
	}
	if limits.TotalTimeout.Duration != defaultTotalTimeout {
		t.Errorf("expected TotalTimeout %v, got %v", defaultTotalTimeout, limits.TotalTimeout.Duration)
	}
	if *limits.MaxConsecutiveFailures != defaultMaxConsecutiveFailures {
		t.Errorf("expected MaxConsecutiveFailures %d, got %d", defaultMaxConsecutiveFailures, *limits.MaxConsecutiveFailures)
	}
}

func TestGetEffectiveLimits_CustomValues(t *testing.T) {
	r := newTestReconciler()
	task := &aiv1alpha1.Task{
		Spec: aiv1alpha1.TaskSpec{
			Limits: &aiv1alpha1.TaskLimits{
				MaxIterations:          ptr.To(int32(50)),
				IterationTimeout:       &metav1.Duration{Duration: 10 * time.Minute},
				TotalTimeout:           &metav1.Duration{Duration: 2 * time.Hour},
				MaxConsecutiveFailures: ptr.To(int32(5)),
			},
		},
	}

	limits := r.getEffectiveLimits(task)

	if *limits.MaxIterations != 50 {
		t.Errorf("expected MaxIterations 50, got %d", *limits.MaxIterations)
	}
	if limits.IterationTimeout.Duration != 10*time.Minute {
		t.Errorf("expected IterationTimeout 10m, got %v", limits.IterationTimeout.Duration)
	}
	if limits.TotalTimeout.Duration != 2*time.Hour {
		t.Errorf("expected TotalTimeout 2h, got %v", limits.TotalTimeout.Duration)
	}
	if *limits.MaxConsecutiveFailures != 5 {
		t.Errorf("expected MaxConsecutiveFailures 5, got %d", *limits.MaxConsecutiveFailures)
	}
}

func TestGetEffectiveLimits_PartialOverrides(t *testing.T) {
	r := newTestReconciler()
	task := &aiv1alpha1.Task{
		Spec: aiv1alpha1.TaskSpec{
			Limits: &aiv1alpha1.TaskLimits{
				MaxIterations: ptr.To(int32(25)),
				// Other fields nil - should use defaults
			},
		},
	}

	limits := r.getEffectiveLimits(task)

	if *limits.MaxIterations != 25 {
		t.Errorf("expected MaxIterations 25, got %d", *limits.MaxIterations)
	}
	// Other fields should be defaults
	if limits.IterationTimeout.Duration != defaultIterationTimeout {
		t.Errorf("expected default IterationTimeout, got %v", limits.IterationTimeout.Duration)
	}
}

func TestLoadTaskSource_Inline(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[{"id":"1","title":"Test Task"}]}`,
			},
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	content, err := r.loadTaskSource(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content != `{"tasks":[{"id":"1","title":"Test Task"}]}` {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestLoadTaskSource_ConfigMap(t *testing.T) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-prd",
			Namespace: "default",
		},
		Data: map[string]string{
			"prd.json": `{"tasks":[{"id":"1","title":"From ConfigMap"}]}`,
		},
	}

	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			TaskSource: aiv1alpha1.TaskSource{
				Type: aiv1alpha1.TaskSourceTypeConfigMap,
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-prd"},
					Key:                  "prd.json",
				},
			},
		},
	}

	r := newTestReconciler(task, configMap)
	ctx := context.Background()

	content, err := r.loadTaskSource(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content != `{"tasks":[{"id":"1","title":"From ConfigMap"}]}` {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestLoadTaskSource_Secret(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-prd-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"prd.json": []byte(`{"tasks":[{"id":"1","title":"From Secret"}]}`),
		},
	}

	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			TaskSource: aiv1alpha1.TaskSource{
				Type: aiv1alpha1.TaskSourceTypeSecret,
				SecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-prd-secret"},
					Key:                  "prd.json",
				},
			},
		},
	}

	r := newTestReconciler(task, secret)
	ctx := context.Background()

	content, err := r.loadTaskSource(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if content != `{"tasks":[{"id":"1","title":"From Secret"}]}` {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestLoadTaskSource_ConfigMapNotFound(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			TaskSource: aiv1alpha1.TaskSource{
				Type: aiv1alpha1.TaskSourceTypeConfigMap,
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent"},
					Key:                  "prd.json",
				},
			},
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	_, err := r.loadTaskSource(ctx, task)
	if err == nil {
		t.Error("expected error for missing configmap")
	}
}

func TestCountTasksInPRD(t *testing.T) {
	tests := []struct {
		name     string
		prd      string
		expected int
	}{
		{
			name:     "empty tasks array",
			prd:      `{"tasks":[]}`,
			expected: 0,
		},
		{
			name:     "single task",
			prd:      `{"tasks":[{"id":"1","title":"Task 1"}]}`,
			expected: 1,
		},
		{
			name:     "multiple tasks",
			prd:      `{"tasks":[{"id":"1","title":"A"},{"id":"2","title":"B"},{"id":"3","title":"C"}]}`,
			expected: 3,
		},
		{
			name:     "invalid json",
			prd:      `not json`,
			expected: 0,
		},
		{
			name:     "no tasks key",
			prd:      `{"title":"No tasks"}`,
			expected: 0,
		},
		{
			name:     "stories instead of tasks",
			prd:      `{"stories":[{"id":"s1","title":"Story 1"},{"id":"s2","title":"Story 2"}]}`,
			expected: 2,
		},
		{
			name:     "tasks with additional fields",
			prd:      `{"tasks":[{"id":"1","title":"Task 1","priority":1,"acceptanceCriteria":["a","b"]},{"id":"2","title":"Task 2","priority":2}]}`,
			expected: 2,
		},
		{
			name:     "nested structure",
			prd:      `{"metadata":{"version":"1.0"},"tasks":[{"id":"1","title":"T1"},{"id":"2","title":"T2"},{"id":"3","title":"T3"},{"id":"4","title":"T4"}]}`,
			expected: 4,
		},
		{
			name:     "empty JSON object",
			prd:      `{}`,
			expected: 0,
		},
		{
			name:     "tasks array with null",
			prd:      `{"tasks":null}`,
			expected: 0,
		},
	}

	r := newTestReconciler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := r.countTasksInPRD(tt.prd)
			if count != tt.expected {
				t.Errorf("expected %d tasks, got %d", tt.expected, count)
			}
		})
	}
}

func TestBuildWorkerEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		agent    *aiv1alpha1.Agent
		expected string
	}{
		{
			name: "basic agent",
			agent: &aiv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "code-worker",
					Namespace: "default",
				},
			},
			expected: "http://code-worker.default:8080",
		},
		{
			name: "different namespace",
			agent: &aiv1alpha1.Agent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker",
					Namespace: "mcp-fabric",
				},
			},
			expected: "http://worker.mcp-fabric:8080",
		},
	}

	r := newTestReconciler()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := r.buildWorkerEndpoint(tt.agent)
			if endpoint != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, endpoint)
			}
		})
	}
}

func TestGetOrchestratorAgent_Default(t *testing.T) {
	// Create the default orchestrator agent
	orchestrator := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultOrchestratorName,
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "orchestrator:v1",
		},
	}

	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			// No OrchestratorRef - should use default
		},
	}

	r := newTestReconciler(task, orchestrator)
	ctx := context.Background()

	agent, err := r.getOrchestratorAgent(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if agent.Name != defaultOrchestratorName {
		t.Errorf("expected agent name %s, got %s", defaultOrchestratorName, agent.Name)
	}
}

func TestGetOrchestratorAgent_CustomRef(t *testing.T) {
	// Create a custom orchestrator agent
	customOrchestrator := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-orchestrator",
			Namespace: "custom-ns",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "custom-orch:v1",
		},
	}

	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			OrchestratorRef: &aiv1alpha1.AgentReference{
				Name:      "custom-orchestrator",
				Namespace: "custom-ns",
			},
		},
	}

	r := newTestReconciler(task, customOrchestrator)
	ctx := context.Background()

	agent, err := r.getOrchestratorAgent(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if agent.Name != "custom-orchestrator" {
		t.Errorf("expected agent name 'custom-orchestrator', got %s", agent.Name)
	}
	if agent.Namespace != "custom-ns" {
		t.Errorf("expected namespace 'custom-ns', got %s", agent.Namespace)
	}
}

func TestReconcileWorkspacePVC(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	err := r.reconcileWorkspacePVC(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify PVC was created
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{
		Name:      "test-task-workspace",
		Namespace: "default",
	}, &pvc); err != nil {
		t.Errorf("failed to get PVC: %v", err)
	}

	if pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("expected ReadWriteOnce, got %v", pvc.Spec.AccessModes)
	}
}

func TestSetCondition(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
	}

	r := newTestReconciler()

	// Add first condition
	r.setCondition(task, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "TestReason",
		Message: "Test message",
	})

	if len(task.Status.Conditions) != 1 {
		t.Errorf("expected 1 condition, got %d", len(task.Status.Conditions))
	}

	// Update the condition
	r.setCondition(task, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionFalse,
		Reason:  "UpdatedReason",
		Message: "Updated message",
	})

	// Should still be 1 condition (updated, not added)
	if len(task.Status.Conditions) != 1 {
		t.Errorf("expected 1 condition after update, got %d", len(task.Status.Conditions))
	}

	// Verify the update
	cond := task.Status.Conditions[0]
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("expected ConditionFalse, got %v", cond.Status)
	}
	if cond.Reason != "UpdatedReason" {
		t.Errorf("expected 'UpdatedReason', got %s", cond.Reason)
	}
}

func TestHandlePendingPhase_MissingOrchestrator(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[{"id":"1","title":"Test"}]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhasePending,
		},
	}

	// Note: No orchestrator agent created - should requeue with delay
	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.handlePendingPhase(ctx, task)

	// The function handles missing orchestrator gracefully - no error returned
	// It sets a condition and requeues after failure delay
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should requeue after failure delay
	if result.RequeueAfter != failureRequeueDelay {
		t.Errorf("expected RequeueAfter %v, got %v", failureRequeueDelay, result.RequeueAfter)
	}

	// Verify condition was set
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}

	// Check that the OrchestratorNotFound condition was set
	found := false
	for _, cond := range updatedTask.Status.Conditions {
		if cond.Type == "Ready" && cond.Reason == "OrchestratorNotFound" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected OrchestratorNotFound condition to be set")
	}
}

func TestHandlePendingPhase_Success(t *testing.T) {
	orchestrator := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultOrchestratorName,
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "orchestrator:v1",
		},
	}

	worker := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "code-worker",
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "worker:v1",
		},
	}

	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "code-worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[{"id":"1","title":"Test"}]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhasePending,
		},
	}

	r := newTestReconciler(task, orchestrator, worker)
	ctx := context.Background()

	result, err := r.handlePendingPhase(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should requeue to poll job status
	if result.RequeueAfter != jobPollInterval {
		t.Errorf("expected RequeueAfter %v, got %v", jobPollInterval, result.RequeueAfter)
	}

	// Verify task status was updated
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}
	if updatedTask.Status.Phase != aiv1alpha1.TaskPhaseRunning {
		t.Errorf("expected phase Running, got %s", updatedTask.Status.Phase)
	}

	// Verify orchestrator job was created
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{
		Name:      "test-task-orchestrator",
		Namespace: "default",
	}, &job); err != nil {
		t.Errorf("failed to get orchestrator job: %v", err)
	}

	// Verify workspace PVC was created
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{
		Name:      "test-task-workspace",
		Namespace: "default",
	}, &pvc); err != nil {
		t.Errorf("failed to get workspace PVC: %v", err)
	}
}

func TestHandleRunningPhase_JobRunning(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhaseRunning,
		},
	}

	// Job still running (no completion condition)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-orchestrator",
			Namespace: "default",
		},
		Status: batchv1.JobStatus{
			Active: 1,
		},
	}

	r := newTestReconciler(task, job)
	ctx := context.Background()

	result, err := r.handleRunningPhase(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Should requeue to poll again
	if result.RequeueAfter != jobPollInterval {
		t.Errorf("expected RequeueAfter %v, got %v", jobPollInterval, result.RequeueAfter)
	}
}

// ==============================================================================
// Finalizer Tests
// ==============================================================================

func TestReconcile_AddsFinalizer(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task",
			Namespace: "default",
			// No finalizer yet
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhasePending,
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result.Requeue {
		t.Error("expected requeue after adding finalizer")
	}

	// Verify finalizer was added
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}

	found := false
	for _, f := range updatedTask.Finalizers {
		if f == taskFinalizer {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected finalizer %s to be added, got finalizers: %v", taskFinalizer, updatedTask.Finalizers)
	}
}

func TestHandleDeletion_CleansUpResources(t *testing.T) {
	now := metav1.Now()
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-task",
			Namespace:         "default",
			Finalizers:        []string{taskFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhaseRunning,
		},
	}

	// Create resources that should be cleaned up
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-workspace",
			Namespace: "default",
		},
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-orchestrator",
			Namespace: "default",
		},
	}

	r := newTestReconciler(task, pvc, job)
	ctx := context.Background()

	result, err := r.handleDeletion(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Error("expected no requeue after successful deletion")
	}

	// Verify PVC was deleted
	var deletedPVC corev1.PersistentVolumeClaim
	err = r.Get(ctx, types.NamespacedName{Name: "test-task-workspace", Namespace: "default"}, &deletedPVC)
	if err == nil {
		t.Error("expected PVC to be deleted")
	}

	// Verify Job was deleted
	var deletedJob batchv1.Job
	err = r.Get(ctx, types.NamespacedName{Name: "test-task-orchestrator", Namespace: "default"}, &deletedJob)
	if err == nil {
		t.Error("expected Job to be deleted")
	}

	// Note: After removing finalizer, the fake client may have already garbage collected
	// the task since it had a deletionTimestamp. In real Kubernetes, this is the expected behavior.
}

func TestHandleDeletion_NoFinalizerNoOp(t *testing.T) {
	now := metav1.Now()
	// Task with a DIFFERENT finalizer (not ours) - this simulates the case where
	// our finalizer was already removed but another controller's finalizer keeps the object alive
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-task",
			Namespace:         "default",
			Finalizers:        []string{"some-other-finalizer"},
			DeletionTimestamp: &now,
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
	}

	r := newTestReconciler(task)
	ctx := context.Background()

	result, err := r.handleDeletion(ctx, task)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Error("expected no requeue when our finalizer not present")
	}

	// Verify that we didn't try to remove the other finalizer
	var updatedTask aiv1alpha1.Task
	if err := r.Get(ctx, types.NamespacedName{Name: "test-task", Namespace: "default"}, &updatedTask); err != nil {
		t.Errorf("failed to get task: %v", err)
	}
	found := false
	for _, f := range updatedTask.Finalizers {
		if f == "some-other-finalizer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected other finalizer to remain unchanged")
	}
}

func TestReconcile_DeletionTriggersCleanup(t *testing.T) {
	now := metav1.Now()
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-task",
			Namespace:         "default",
			Finalizers:        []string{taskFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
		Status: aiv1alpha1.TaskStatus{
			Phase: aiv1alpha1.TaskPhaseRunning,
		},
	}

	// Create resources that should be cleaned up
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-task-workspace",
			Namespace: "default",
		},
	}

	r := newTestReconciler(task, pvc)
	ctx := context.Background()

	// Reconcile should trigger deletion handling
	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-task",
			Namespace: "default",
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Error("expected no requeue after deletion cleanup")
	}

	// Verify PVC was deleted as part of cleanup
	var deletedPVC corev1.PersistentVolumeClaim
	err = r.Get(ctx, types.NamespacedName{Name: "test-task-workspace", Namespace: "default"}, &deletedPVC)
	if err == nil {
		t.Error("expected PVC to be deleted during deletion cleanup")
	}

	// Note: After removing finalizer, the fake client may have already garbage collected
	// the task since it had a deletionTimestamp. In real Kubernetes, this is the expected behavior.
}

// ==============================================================================
// Log Extraction Tests
// ==============================================================================

func TestParseOrchestratorResultFromLogs(t *testing.T) {
	// Test the log parsing logic by testing the extraction of JSON from log strings.
	// Since getOrchestratorResult requires a kubernetes.Clientset to fetch pod logs,
	// we test the parsing logic separately using a helper that extracts the JSON.

	tests := []struct {
		name        string
		logContent  string
		wantResult  *OrchestratorResult
		wantErr     bool
		errContains string
	}{
		{
			name: "successful result with all fields",
			logContent: `Starting orchestrator...
Processing task...
ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":5,"totalTasks":5,"iterations":3,"learnings":"Completed all tasks","commitSha":"abc123","pullRequestUrl":"https://github.com/org/repo/pull/1"}
Done.`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 5,
				TotalTasks:     5,
				Iterations:     3,
				Learnings:      "Completed all tasks",
				CommitSHA:      "abc123",
				PullRequestURL: "https://github.com/org/repo/pull/1",
			},
			wantErr: false,
		},
		{
			name: "failed result with error",
			logContent: `Starting orchestrator...
Error occurred!
ORCHESTRATOR_RESULT:{"passed":false,"completedTasks":2,"totalTasks":5,"iterations":10,"error":"Max iterations exceeded","learnings":"Partial progress made"}`,
			wantResult: &OrchestratorResult{
				Passed:         false,
				CompletedTasks: 2,
				TotalTasks:     5,
				Iterations:     10,
				Error:          "Max iterations exceeded",
				Learnings:      "Partial progress made",
			},
			wantErr: false,
		},
		{
			name: "result with no changes",
			logContent: `ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":0,"totalTasks":0,"iterations":1,"noChanges":true,"learnings":"No changes needed"}`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 0,
				TotalTasks:     0,
				Iterations:     1,
				NoChanges:      true,
				Learnings:      "No changes needed",
			},
			wantErr: false,
		},
		{
			name: "result with git push info",
			logContent: `ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":3,"totalTasks":3,"iterations":2,"pushed":true,"commitSha":"def456"}`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 3,
				TotalTasks:     3,
				Iterations:     2,
				Pushed:         true,
				CommitSHA:      "def456",
			},
			wantErr: false,
		},
		{
			name: "result with git error",
			logContent: `ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":3,"totalTasks":3,"iterations":2,"pushed":false,"gitError":"Permission denied"}`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 3,
				TotalTasks:     3,
				Iterations:     2,
				Pushed:         false,
				GitError:       "Permission denied",
			},
			wantErr: false,
		},
		{
			name:        "missing result marker",
			logContent:  "Some logs without the result marker",
			wantResult:  nil,
			wantErr:     true,
			errContains: "marker not found",
		},
		{
			name:        "invalid JSON after marker",
			logContent:  "ORCHESTRATOR_RESULT:{invalid json}",
			wantResult:  nil,
			wantErr:     true,
			errContains: "failed to parse",
		},
		{
			name:        "empty logs",
			logContent:  "",
			wantResult:  nil,
			wantErr:     true,
			errContains: "marker not found",
		},
		{
			name: "multiple result markers - uses last one",
			logContent: `ORCHESTRATOR_RESULT:{"passed":false,"completedTasks":1,"totalTasks":5,"iterations":1}
More processing...
ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":5,"totalTasks":5,"iterations":3}`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 5,
				TotalTasks:     5,
				Iterations:     3,
			},
			wantErr: false,
		},
		{
			name: "result with PRD field",
			logContent: `ORCHESTRATOR_RESULT:{"passed":true,"completedTasks":2,"totalTasks":2,"iterations":1,"prd":{"tasks":[{"id":"1","passes":true}]}}`,
			wantResult: &OrchestratorResult{
				Passed:         true,
				CompletedTasks: 2,
				TotalTasks:     2,
				Iterations:     1,
				PRD:            []byte(`{"tasks":[{"id":"1","passes":true}]}`),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseOrchestratorResultFromLogs(tt.logContent)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Passed != tt.wantResult.Passed {
				t.Errorf("Passed: got %v, want %v", result.Passed, tt.wantResult.Passed)
			}
			if result.CompletedTasks != tt.wantResult.CompletedTasks {
				t.Errorf("CompletedTasks: got %d, want %d", result.CompletedTasks, tt.wantResult.CompletedTasks)
			}
			if result.TotalTasks != tt.wantResult.TotalTasks {
				t.Errorf("TotalTasks: got %d, want %d", result.TotalTasks, tt.wantResult.TotalTasks)
			}
			if result.Iterations != tt.wantResult.Iterations {
				t.Errorf("Iterations: got %d, want %d", result.Iterations, tt.wantResult.Iterations)
			}
			if result.Learnings != tt.wantResult.Learnings {
				t.Errorf("Learnings: got %q, want %q", result.Learnings, tt.wantResult.Learnings)
			}
			if result.CommitSHA != tt.wantResult.CommitSHA {
				t.Errorf("CommitSHA: got %q, want %q", result.CommitSHA, tt.wantResult.CommitSHA)
			}
			if result.PullRequestURL != tt.wantResult.PullRequestURL {
				t.Errorf("PullRequestURL: got %q, want %q", result.PullRequestURL, tt.wantResult.PullRequestURL)
			}
			if result.Error != tt.wantResult.Error {
				t.Errorf("Error: got %q, want %q", result.Error, tt.wantResult.Error)
			}
			if result.NoChanges != tt.wantResult.NoChanges {
				t.Errorf("NoChanges: got %v, want %v", result.NoChanges, tt.wantResult.NoChanges)
			}
			if result.Pushed != tt.wantResult.Pushed {
				t.Errorf("Pushed: got %v, want %v", result.Pushed, tt.wantResult.Pushed)
			}
			if result.GitError != tt.wantResult.GitError {
				t.Errorf("GitError: got %q, want %q", result.GitError, tt.wantResult.GitError)
			}
			// Compare PRD as strings since json.RawMessage comparison can be tricky
			if string(result.PRD) != string(tt.wantResult.PRD) {
				t.Errorf("PRD: got %s, want %s", string(result.PRD), string(tt.wantResult.PRD))
			}
		})
	}
}
