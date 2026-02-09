package controllers

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
)

func newAgentTestReconciler(objs ...client.Object) *AgentReconciler {
	scheme := runtime.NewScheme()
	_ = aiv1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&aiv1alpha1.Agent{}).
		Build()

	return &AgentReconciler{Client: fakeClient, Scheme: scheme}
}

func newWorkerAgent(standalone *bool) *aiv1alpha1.Agent {
	return &aiv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: "code-worker", Namespace: "default"},
		Spec: aiv1alpha1.AgentSpec{
			Prompt:     "do work",
			Image:      "worker:v1",
			Standalone: standalone,
			Model:      aiv1alpha1.ModelConfig{Provider: "bedrock", ModelID: "amazon.nova-lite-v1:0"},
		},
	}
}

func TestAgentReconcile_NonStandalone_SkipsAndCleansWorkload(t *testing.T) {
	agent := newWorkerAgent(ptr.To(false))

	// Pre-existing standalone workload from a prior configuration.
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "code-worker", Namespace: "default"}}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "code-worker", Namespace: "default"}}

	r := newAgentTestReconciler(agent, dep, svc)
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "code-worker", Namespace: "default"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Deployment and Service must be removed.
	var gotDep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &gotDep); !apierrors.IsNotFound(err) {
		t.Errorf("expected Deployment to be deleted, got err=%v", err)
	}
	var gotSvc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &gotSvc); !apierrors.IsNotFound(err) {
		t.Errorf("expected Service to be deleted, got err=%v", err)
	}

	// But the ServiceAccount (needed for the sidecar / IRSA) must exist.
	var sa corev1.ServiceAccount
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &sa); err != nil {
		t.Errorf("expected ServiceAccount to be created, got err=%v", err)
	}

	// Status reflects a ready worker with no standalone endpoint.
	var got aiv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &got); err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if !got.Status.Ready {
		t.Error("expected non-standalone agent to be Ready")
	}
	if got.Status.Endpoint != "" {
		t.Errorf("expected empty endpoint for non-standalone agent, got %q", got.Status.Endpoint)
	}
}

func TestAgentReconcile_Standalone_CreatesWorkload(t *testing.T) {
	agent := newWorkerAgent(nil) // nil => standalone defaults to true

	r := newAgentTestReconciler(agent)
	ctx := context.Background()

	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "code-worker", Namespace: "default"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var dep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &dep); err != nil {
		t.Errorf("expected Deployment to be created for standalone agent, got err=%v", err)
	}
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &svc); err != nil {
		t.Errorf("expected Service to be created for standalone agent, got err=%v", err)
	}

	var got aiv1alpha1.Agent
	if err := r.Get(ctx, types.NamespacedName{Name: "code-worker", Namespace: "default"}, &got); err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if got.Status.Endpoint == "" {
		t.Error("expected standalone agent to publish an endpoint")
	}
}
