package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	"github.com/jarsater/mcp-fabric/operator/internal/metrics"
	"github.com/jarsater/mcp-fabric/operator/internal/render"
)

// AgentReconciler reconciles an Agent object.
type AgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents/finalizers,verbs=update
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=tools,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Agent reconciliation.
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	// Fetch the Agent
	var agent aiv1alpha1.Agent
	if err := r.Get(ctx, req.NamespacedName, &agent); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Agent was deleted, clean up metrics
			metrics.DeleteAgentMetrics(req.Name, req.Namespace)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling Agent", "name", agent.Name)

	// Resolve Tools
	toolPackages, err := r.resolveToolPackages(ctx, &agent)
	if err != nil {
		r.setCondition(&agent, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: agent.Generation,
			Reason:             "ToolResolutionFailed",
			Message:            err.Error(),
		})
		agent.Status.Ready = false
		if statusErr := r.Status().Update(ctx, &agent); statusErr != nil {
			metrics.RecordReconcile(metrics.ControllerAgent, metrics.ResultError, time.Since(startTime).Seconds())
			metrics.RecordReconcileError(metrics.ControllerAgent, "status_update")
			return ctrl.Result{}, statusErr
		}
		metrics.RecordReconcile(metrics.ControllerAgent, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerAgent, "tool_resolution")
		return ctrl.Result{}, err
	}

	// Resolve MCP endpoints (placeholder - would query MCPServer CRs)
	mcpEndpoints := r.resolveMCPEndpoints(ctx, &agent)
	agent.Status.ResolvedMCPEndpoints = mcpEndpoints

	// Standard labels for all resources
	agentLabels := render.AgentLabels(&agent)

	// Create/Update ServiceAccount
	if err := r.reconcileServiceAccount(ctx, &agent, agentLabels); err != nil {
		return ctrl.Result{}, err
	}

	// Create/Update ConfigMap
	configHash, err := r.reconcileConfigMap(ctx, &agent, toolPackages, mcpEndpoints, agentLabels)
	if err != nil {
		return ctrl.Result{}, err
	}
	agent.Status.ConfigHash = configHash

	// Create/Update Deployment
	if err := r.reconcileDeployment(ctx, &agent, configHash, agentLabels, toolPackages); err != nil {
		return ctrl.Result{}, err
	}

	// Create/Update Service
	if err := r.reconcileService(ctx, &agent, agentLabels); err != nil {
		return ctrl.Result{}, err
	}

	// Update status
	agent.Status.Endpoint = render.AgentEndpoint(&agent)
	agent.Status.ObservedGeneration = agent.Generation

	// Check deployment readiness
	ready, replicas := r.checkDeploymentReady(ctx, &agent)
	agent.Status.Ready = ready
	agent.Status.AvailableReplicas = replicas

	// Populate available tools from spec when agent is ready
	if ready && len(agent.Spec.Tools) > 0 {
		agent.Status.AvailableTools = agent.Spec.Tools
	} else if !ready {
		agent.Status.AvailableTools = nil
	}

	if ready {
		r.setCondition(&agent, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: agent.Generation,
			Reason:             "DeploymentReady",
			Message:            "Agent deployment is ready",
		})
	} else {
		r.setCondition(&agent, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: agent.Generation,
			Reason:             "DeploymentNotReady",
			Message:            "Agent deployment is not yet ready",
		})
	}

	if err := r.Status().Update(ctx, &agent); err != nil {
		metrics.RecordReconcile(metrics.ControllerAgent, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerAgent, "status_update")
		return ctrl.Result{}, err
	}

	// Record agent metrics
	modelID := ""
	if agent.Spec.Model.ModelID != "" {
		modelID = agent.Spec.Model.ModelID
	}
	image := render.DefaultAgentRunnerImage
	if agent.Spec.Image != "" {
		image = agent.Spec.Image
	}
	desiredReplicas := int32(1)
	if agent.Spec.Replicas != nil {
		desiredReplicas = *agent.Spec.Replicas
	}
	toolsCount := len(agent.Status.AvailableTools)
	metrics.SetAgentMetrics(agent.Name, agent.Namespace, modelID, image, ready, int(desiredReplicas), int(agent.Status.AvailableReplicas), toolsCount)

	// Record reconciliation success
	metrics.RecordReconcile(metrics.ControllerAgent, metrics.ResultSuccess, time.Since(startTime).Seconds())

	logger.Info("Agent reconciled", "name", agent.Name, "ready", ready)
	return ctrl.Result{}, nil
}

// resolveToolPackages fetches and validates referenced Tools.
func (r *AgentReconciler) resolveToolPackages(ctx context.Context, agent *aiv1alpha1.Agent) ([]render.ToolPackageInfo, error) {
	var result []render.ToolPackageInfo

	for _, ref := range agent.Spec.ToolPackages {
		ns := ref.Namespace
		if ns == "" {
			ns = agent.Namespace
		}

		var tool aiv1alpha1.Tool
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &tool); err != nil {
			return nil, fmt.Errorf("failed to get Tool %s/%s: %w", ns, ref.Name, err)
		}

		if !tool.Status.Ready {
			return nil, fmt.Errorf("tool %s/%s is not ready", ns, ref.Name)
		}

		result = append(result, render.ToolPackageInfo{
			Name:          tool.Name,
			Namespace:     tool.Namespace,
			Image:         tool.Spec.Image,
			EntryModule:   tool.Spec.EntryModule,
			EnabledTools:  ref.EnabledTools,
			DisabledTools: ref.DisabledTools,
		})
	}

	return result, nil
}

// resolveMCPEndpoints discovers MCP servers matching the agent's selector.
func (r *AgentReconciler) resolveMCPEndpoints(ctx context.Context, agent *aiv1alpha1.Agent) []aiv1alpha1.ResolvedMCPEndpoint {
	// Placeholder: would query MCPServer CRs based on agent.Spec.MCPSelector
	// For now, return empty list
	return nil
}

func (r *AgentReconciler) reconcileServiceAccount(ctx context.Context, agent *aiv1alpha1.Agent, agentLabels map[string]string) error {
	// Skip if using a custom SA
	if agent.Spec.ServiceAccountName != "" {
		return nil
	}

	sa := render.AgentServiceAccount(agent, agentLabels)
	if err := controllerutil.SetControllerReference(agent, sa, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, sa)
	} else if err != nil {
		return err
	}

	// Update if needed
	existing.Labels = sa.Labels
	return r.Update(ctx, existing)
}

func (r *AgentReconciler) reconcileConfigMap(ctx context.Context, agent *aiv1alpha1.Agent, toolPackages []render.ToolPackageInfo, mcpEndpoints []aiv1alpha1.ResolvedMCPEndpoint, agentLabels map[string]string) (string, error) {
	// Convert MCP endpoints to render format
	var renderMCPEndpoints []render.AgentMCPEndpoint
	for _, ep := range mcpEndpoints {
		renderMCPEndpoints = append(renderMCPEndpoints, render.AgentMCPEndpoint{
			Name:      ep.Name,
			Namespace: ep.Namespace,
			Endpoint:  ep.Endpoint,
		})
	}

	cm, configJSON, err := render.AgentConfigMap(render.AgentConfigMapParams{
		Agent:        agent,
		ToolPackages: toolPackages,
		MCPEndpoints: renderMCPEndpoints,
		Labels:       agentLabels,
	})
	if err != nil {
		return "", err
	}

	configHash := render.HashConfig(configJSON)

	if err := controllerutil.SetControllerReference(agent, cm, r.Scheme); err != nil {
		return "", err
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existing)
	if errors.IsNotFound(err) {
		return configHash, r.Create(ctx, cm)
	} else if err != nil {
		return "", err
	}

	existing.Data = cm.Data
	existing.Labels = cm.Labels
	return configHash, r.Update(ctx, existing)
}

func (r *AgentReconciler) reconcileDeployment(ctx context.Context, agent *aiv1alpha1.Agent, configHash string, agentLabels map[string]string, toolPackages []render.ToolPackageInfo) error {
	deployment := render.AgentDeployment(render.AgentDeploymentParams{
		Agent:         agent,
		ConfigMapName: agent.Name + "-config",
		ConfigHash:    configHash,
		Labels:        agentLabels,
		ToolPackages:  toolPackages,
	})

	if err := controllerutil.SetControllerReference(agent, deployment, r.Scheme); err != nil {
		return err
	}

	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	// Update deployment spec
	existing.Spec = deployment.Spec
	existing.Labels = deployment.Labels
	return r.Update(ctx, existing)
}

func (r *AgentReconciler) reconcileService(ctx context.Context, agent *aiv1alpha1.Agent, agentLabels map[string]string) error {
	svc := render.AgentService(agent, agentLabels)

	if err := controllerutil.SetControllerReference(agent, svc, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, svc)
	} else if err != nil {
		return err
	}

	// Preserve ClusterIP
	svc.Spec.ClusterIP = existing.Spec.ClusterIP
	existing.Spec = svc.Spec
	existing.Labels = svc.Labels
	return r.Update(ctx, existing)
}

func (r *AgentReconciler) checkDeploymentReady(ctx context.Context, agent *aiv1alpha1.Agent) (bool, int32) {
	var deployment appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: agent.Name, Namespace: agent.Namespace}, &deployment); err != nil {
		return false, 0
	}

	replicas := int32(1)
	if agent.Spec.Replicas != nil {
		replicas = *agent.Spec.Replicas
	}

	ready := deployment.Status.ReadyReplicas >= replicas && deployment.Status.ReadyReplicas > 0
	return ready, deployment.Status.ReadyReplicas
}

func (r *AgentReconciler) setCondition(agent *aiv1alpha1.Agent, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&agent.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.Agent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Named("agent").
		Complete(r)
}
