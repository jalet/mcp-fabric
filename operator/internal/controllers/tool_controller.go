package controllers

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	"github.com/jarsater/mcp-fabric/operator/internal/metrics"
)

// ToolReconciler reconciles a Tool object.
type ToolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tools/finalizers,verbs=update

// Reconcile handles Tool reconciliation.
func (r *ToolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	// Fetch the Tool
	var tool aiv1alpha1.Tool
	if err := r.Get(ctx, req.NamespacedName, &tool); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Tool was deleted, clean up metrics
			metrics.DeleteToolMetrics(req.Name, req.Namespace)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling Tool", "name", tool.Name)

	// Validate the spec
	if err := r.validateSpec(&tool); err != nil {
		r.setCondition(&tool, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: tool.Generation,
			Reason:             "ValidationFailed",
			Message:            err.Error(),
		})
		tool.Status.Ready = false
		if err := r.Status().Update(ctx, &tool); err != nil {
			metrics.RecordReconcile(metrics.ControllerTool, metrics.ResultError, time.Since(startTime).Seconds())
			metrics.RecordReconcileError(metrics.ControllerTool, "status_update")
			return ctrl.Result{}, err
		}
		metrics.SetToolMetrics(tool.Name, tool.Namespace, false, 0)
		metrics.RecordReconcile(metrics.ControllerTool, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerTool, "validation")
		return ctrl.Result{}, nil
	}

	// Copy declared tools to available tools (in the future, introspection Job would populate this)
	tool.Status.AvailableTools = tool.Spec.Tools

	// Set ready condition
	r.setCondition(&tool, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: tool.Generation,
		Reason:             "Validated",
		Message:            "Tool is valid and ready",
	})

	tool.Status.Ready = true
	tool.Status.ObservedGeneration = tool.Generation

	if err := r.Status().Update(ctx, &tool); err != nil {
		metrics.RecordReconcile(metrics.ControllerTool, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerTool, "status_update")
		return ctrl.Result{}, err
	}

	// Record metrics
	metrics.SetToolMetrics(tool.Name, tool.Namespace, true, len(tool.Status.AvailableTools))
	metrics.RecordReconcile(metrics.ControllerTool, metrics.ResultSuccess, time.Since(startTime).Seconds())

	logger.Info("Tool reconciled successfully", "name", tool.Name, "tools", len(tool.Status.AvailableTools))
	return ctrl.Result{}, nil
}

// validateSpec performs validation on the Tool spec.
func (r *ToolReconciler) validateSpec(t *aiv1alpha1.Tool) error {
	if t.Spec.Image == "" {
		return fmt.Errorf("spec.image is required")
	}
	return nil
}

// setCondition sets or updates a condition on the Tool status.
func (r *ToolReconciler) setCondition(t *aiv1alpha1.Tool, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&t.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ToolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.Tool{}).
		Named("tool").
		Complete(r)
}
