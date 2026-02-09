//go:build integration

package controllers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestTaskControllerIntegration runs integration tests using envtest.
// Run with: go test -tags=integration ./internal/controllers/... -v
func TestTaskControllerIntegration(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(t)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup envtest
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	}()

	// Register scheme
	err = aiv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	err = batchv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("failed to add batch scheme: %v", err)
	}

	// Create manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Setup reconciler
	reconciler := &TaskReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		t.Fatalf("failed to setup reconciler: %v", err)
	}

	// Start manager in background
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("failed to start manager: %v", err)
		}
	}()

	// Wait for cache to sync
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("failed to sync cache")
	}

	k8sClient := mgr.GetClient()

	// Run sub-tests
	t.Run("TaskLifecycle", func(t *testing.T) {
		testTaskLifecycle(t, ctx, k8sClient, cfg)
	})

	t.Run("TaskPauseResume", func(t *testing.T) {
		testTaskPauseResume(t, ctx, k8sClient)
	})

	t.Run("TaskDeletion", func(t *testing.T) {
		testTaskDeletion(t, ctx, k8sClient)
	})
}

// testTaskLifecycle tests the basic task lifecycle from Pending to Running.
func testTaskLifecycle(t *testing.T, ctx context.Context, k8sClient client.Client, cfg *rest.Config) {
	// Create orchestrator agent
	orchestrator := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-orchestrator",
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "test-orchestrator:v1",
		},
	}
	if err := k8sClient.Create(ctx, orchestrator); err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, orchestrator)
	}()

	// Create worker agent
	worker := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "code-worker",
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "test-worker:v1",
		},
	}
	if err := k8sClient.Create(ctx, worker); err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, worker)
	}()

	// Create task
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lifecycle-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "code-worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[{"id":"1","title":"Test Task"}]}`,
			},
		},
	}
	if err := k8sClient.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, task)
	}()

	// Wait for task to transition to Running
	taskKey := types.NamespacedName{Name: task.Name, Namespace: task.Namespace}
	var updatedTask aiv1alpha1.Task

	// Poll for status update with timeout
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for task to transition to Running, current phase: %s", updatedTask.Status.Phase)
		case <-ticker.C:
			if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
				continue
			}
			if updatedTask.Status.Phase == aiv1alpha1.TaskPhaseRunning {
				// Success - task transitioned to Running
				return
			}
		}
	}
}

// testTaskPauseResume tests pausing and resuming a task.
func testTaskPauseResume(t *testing.T, ctx context.Context, k8sClient client.Client) {
	// Create task
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pause-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "code-worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[{"id":"1","title":"Test"}]}`,
			},
			Paused: true, // Start paused
		},
	}
	if err := k8sClient.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, task)
	}()

	taskKey := types.NamespacedName{Name: task.Name, Namespace: task.Namespace}
	var updatedTask aiv1alpha1.Task

	// Wait for task to be in Paused phase
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for task to be Paused, current phase: %s", updatedTask.Status.Phase)
		case <-ticker.C:
			if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
				continue
			}
			if updatedTask.Status.Phase == aiv1alpha1.TaskPhasePaused {
				goto ResumeTest
			}
		}
	}

ResumeTest:
	// Now unpause the task
	if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	updatedTask.Spec.Paused = false
	if err := k8sClient.Update(ctx, &updatedTask); err != nil {
		t.Fatalf("failed to update task: %v", err)
	}

	// Wait for task to resume (transition away from Paused)
	timeout = time.After(30 * time.Second)
	ticker = time.NewTicker(500 * time.Millisecond)

	for {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for task to resume, current phase: %s", updatedTask.Status.Phase)
		case <-ticker.C:
			if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
				continue
			}
			if updatedTask.Status.Phase != aiv1alpha1.TaskPhasePaused {
				// Success - task resumed
				return
			}
		}
	}
}

// testTaskDeletion tests that task deletion properly cleans up resources.
func testTaskDeletion(t *testing.T, ctx context.Context, k8sClient client.Client) {
	// Create task
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deletion-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "code-worker"},
			TaskSource: aiv1alpha1.TaskSource{
				Type:   aiv1alpha1.TaskSourceTypeInline,
				Inline: `{"tasks":[]}`,
			},
		},
	}
	if err := k8sClient.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	taskKey := types.NamespacedName{Name: task.Name, Namespace: task.Namespace}
	var updatedTask aiv1alpha1.Task

	// Wait for finalizer to be added
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for finalizer to be added")
		case <-ticker.C:
			if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
				continue
			}
			for _, f := range updatedTask.Finalizers {
				if f == taskFinalizer {
					goto DeleteTask
				}
			}
		}
	}

DeleteTask:
	// Delete the task
	if err := k8sClient.Delete(ctx, &updatedTask); err != nil {
		t.Fatalf("failed to delete task: %v", err)
	}

	// Wait for task to be fully deleted
	timeout = time.After(30 * time.Second)
	ticker = time.NewTicker(500 * time.Millisecond)

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for task to be deleted")
		case <-ticker.C:
			err := k8sClient.Get(ctx, taskKey, &updatedTask)
			if err != nil {
				// Task was deleted - success
				return
			}
		}
	}
}

// TestTaskReconcilerWithConfigMapSource tests task creation with ConfigMap PRD source.
func TestTaskReconcilerWithConfigMapSource(t *testing.T) {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(t)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup envtest
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("failed to stop envtest: %v", err)
		}
	}()

	// Register scheme
	err = aiv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	// Create manager
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Setup reconciler
	reconciler := &TaskReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		t.Fatalf("failed to setup reconciler: %v", err)
	}

	// Start manager in background
	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Errorf("failed to start manager: %v", err)
		}
	}()

	// Wait for cache to sync
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("failed to sync cache")
	}

	k8sClient := mgr.GetClient()

	// Create ConfigMap with PRD
	prdConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-prd-configmap",
			Namespace: "default",
		},
		Data: map[string]string{
			"prd.json": `{"tasks":[{"id":"task-1","title":"Implement feature A"},{"id":"task-2","title":"Implement feature B"}]}`,
		},
	}
	if err := k8sClient.Create(ctx, prdConfigMap); err != nil {
		t.Fatalf("failed to create configmap: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, prdConfigMap)
	}()

	// Create orchestrator and worker agents
	orchestrator := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-orchestrator",
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "test-orchestrator:v1",
		},
	}
	if err := k8sClient.Create(ctx, orchestrator); err != nil {
		// May already exist from previous test
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: orchestrator.Name, Namespace: orchestrator.Namespace}, orchestrator); err != nil {
			t.Fatalf("failed to get or create orchestrator: %v", err)
		}
	}
	defer func() {
		_ = k8sClient.Delete(ctx, orchestrator)
	}()

	worker := &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "code-worker-cm",
			Namespace: "default",
		},
		Spec: aiv1alpha1.AgentSpec{
			Image: "test-worker:v1",
		},
	}
	if err := k8sClient.Create(ctx, worker); err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, worker)
	}()

	// Create task with ConfigMap source
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap-task",
			Namespace: "default",
		},
		Spec: aiv1alpha1.TaskSpec{
			WorkerRef: aiv1alpha1.AgentReference{Name: "code-worker-cm"},
			TaskSource: aiv1alpha1.TaskSource{
				Type: aiv1alpha1.TaskSourceTypeConfigMap,
				ConfigMapRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-prd-configmap"},
					Key:                  "prd.json",
				},
			},
		},
	}
	if err := k8sClient.Create(ctx, task); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	defer func() {
		_ = k8sClient.Delete(ctx, task)
	}()

	// Wait for task to have TotalTasks populated from ConfigMap
	taskKey := types.NamespacedName{Name: task.Name, Namespace: task.Namespace}
	var updatedTask aiv1alpha1.Task

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting for task to read TotalTasks from ConfigMap, current: %d", updatedTask.Status.TotalTasks)
		case <-ticker.C:
			if err := k8sClient.Get(ctx, taskKey, &updatedTask); err != nil {
				continue
			}
			// Task should have parsed 2 tasks from the ConfigMap
			if updatedTask.Status.TotalTasks == 2 {
				return // Success
			}
		}
	}
}
