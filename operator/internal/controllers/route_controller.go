package controllers

import (
	"context"
	"sort"
	"time"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	"github.com/jarsater/mcp-fabric/operator/internal/metrics"
	"github.com/jarsater/mcp-fabric/operator/internal/render"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// RouteReconciler reconciles a Route object.
type RouteReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	GatewayNamespace string // Namespace where gateway routes ConfigMap is created
}

// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=routes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=routes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=routes/finalizers,verbs=update
// +kubebuilder:rbac:groups=fabric.jarsater.ai,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles Route reconciliation.
func (r *RouteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := time.Now()
	logger := log.FromContext(ctx)

	// Fetch the Route
	var route aiv1alpha1.Route
	if err := r.Get(ctx, req.NamespacedName, &route); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Route was deleted, clean up metrics
			metrics.DeleteRouteMetrics(req.Name, req.Namespace)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling Route", "name", route.Name)

	// Resolve all backend agents
	backends, allReady := r.resolveBackends(ctx, &route)
	route.Status.Backends = backends

	// Compile routing config
	routeConfig := r.compileRouteConfig(&route, backends)

	// Update the gateway routes ConfigMap
	gatewayNS := r.GatewayNamespace
	if gatewayNS == "" {
		gatewayNS = render.GatewayNamespace
	}

	if err := r.reconcileRoutesConfigMap(ctx, gatewayNS, routeConfig); err != nil {
		r.setCondition(&route, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: route.Generation,
			Reason:             "ConfigMapUpdateFailed",
			Message:            err.Error(),
		})
		route.Status.Ready = false
		if statusErr := r.Status().Update(ctx, &route); statusErr != nil {
			// Handle optimistic concurrency conflicts gracefully
			if errors.IsConflict(statusErr) {
				logger.V(1).Info("Conflict updating Route status, will retry", "name", route.Name)
				metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultRequeue, time.Since(startTime).Seconds())
				return ctrl.Result{Requeue: true}, nil
			}
			metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultError, time.Since(startTime).Seconds())
			metrics.RecordReconcileError(metrics.ControllerRoute, "status_update")
			return ctrl.Result{}, statusErr
		}
		metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerRoute, "configmap_update")
		return ctrl.Result{}, err
	}

	// Update status
	route.Status.ActiveRules = int32(len(route.Spec.Rules))
	route.Status.CompiledConfigMap = "mcp-fabric-gateway-routes"
	route.Status.ObservedGeneration = route.Generation
	route.Status.Ready = allReady

	if allReady {
		r.setCondition(&route, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: route.Generation,
			Reason:             "AllBackendsReady",
			Message:            "All backend agents are ready",
		})
	} else {
		r.setCondition(&route, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: route.Generation,
			Reason:             "BackendsNotReady",
			Message:            "Some backend agents are not ready",
		})
	}

	if err := r.Status().Update(ctx, &route); err != nil {
		// Handle optimistic concurrency conflicts gracefully - just requeue
		if errors.IsConflict(err) {
			logger.V(1).Info("Conflict updating Route status, will retry", "name", route.Name)
			metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultRequeue, time.Since(startTime).Seconds())
			return ctrl.Result{Requeue: true}, nil
		}
		metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultError, time.Since(startTime).Seconds())
		metrics.RecordReconcileError(metrics.ControllerRoute, "status_update")
		return ctrl.Result{}, err
	}

	// Count ready backends
	readyBackends := 0
	for _, b := range backends {
		if b.Ready {
			readyBackends++
		}
	}

	// Record metrics
	metrics.SetRouteMetrics(route.Name, route.Namespace, len(route.Spec.Rules), readyBackends)
	metrics.RecordReconcile(metrics.ControllerRoute, metrics.ResultSuccess, time.Since(startTime).Seconds())

	logger.Info("Route reconciled", "name", route.Name, "rules", route.Status.ActiveRules, "ready", route.Status.Ready)
	return ctrl.Result{}, nil
}

// resolveBackends fetches all referenced agents and returns their status.
func (r *RouteReconciler) resolveBackends(ctx context.Context, route *aiv1alpha1.Route) ([]aiv1alpha1.BackendStatus, bool) {
	var backends []aiv1alpha1.BackendStatus
	allReady := true

	seen := make(map[string]bool)

	// Collect all backends from rules
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.Backends {
			key := backend.AgentRef.Namespace + "/" + backend.AgentRef.Name
			if seen[key] {
				continue
			}
			seen[key] = true

			ns := backend.AgentRef.Namespace
			if ns == "" {
				ns = route.Namespace
			}

			var agent aiv1alpha1.Agent
			err := r.Get(ctx, types.NamespacedName{Name: backend.AgentRef.Name, Namespace: ns}, &agent)

			status := aiv1alpha1.BackendStatus{
				AgentRef: aiv1alpha1.AgentRef{
					Name:      backend.AgentRef.Name,
					Namespace: ns,
				},
			}

			if err != nil {
				status.Ready = false
				allReady = false
			} else {
				status.Ready = agent.Status.Ready
				status.Endpoint = agent.Status.Endpoint
				if !agent.Status.Ready {
					allReady = false
				}
			}

			backends = append(backends, status)
		}
	}

	// Check default backend
	if route.Spec.Defaults != nil && route.Spec.Defaults.Backend != nil {
		ref := route.Spec.Defaults.Backend.AgentRef
		key := ref.Namespace + "/" + ref.Name
		if !seen[key] {
			ns := ref.Namespace
			if ns == "" {
				ns = route.Namespace
			}

			var agent aiv1alpha1.Agent
			err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: ns}, &agent)

			status := aiv1alpha1.BackendStatus{
				AgentRef: aiv1alpha1.AgentRef{
					Name:      ref.Name,
					Namespace: ns,
				},
			}

			if err != nil {
				status.Ready = false
				allReady = false
			} else {
				status.Ready = agent.Status.Ready
				status.Endpoint = agent.Status.Endpoint
				if !agent.Status.Ready {
					allReady = false
				}
			}

			backends = append(backends, status)
		}
	}

	return backends, allReady
}

// compileRouteConfig transforms Route into the gateway-consumable format.
func (r *RouteReconciler) compileRouteConfig(route *aiv1alpha1.Route, backends []aiv1alpha1.BackendStatus) *render.RouteConfig {
	// Create a lookup map for backend status
	backendMap := make(map[string]aiv1alpha1.BackendStatus)
	for _, b := range backends {
		key := b.AgentRef.Namespace + "/" + b.AgentRef.Name
		backendMap[key] = b
	}

	config := &render.RouteConfig{
		Rules: make([]render.CompiledRouteRule, 0, len(route.Spec.Rules)),
	}

	// Compile rules
	for _, rule := range route.Spec.Rules {
		compiled := render.CompiledRouteRule{
			Name:     rule.Name,
			Priority: 0,
			Match: render.CompiledRouteMatch{
				Agent:       rule.Match.Agent,
				IntentRegex: rule.Match.IntentRegex,
				TenantID:    rule.Match.TenantID,
				Headers:     rule.Match.Headers,
			},
			Backends: make([]render.CompiledRouteBackend, 0, len(rule.Backends)),
		}

		if rule.Priority != nil {
			compiled.Priority = *rule.Priority
		}

		for _, backend := range rule.Backends {
			ns := backend.AgentRef.Namespace
			if ns == "" {
				ns = route.Namespace
			}
			key := ns + "/" + backend.AgentRef.Name
			status := backendMap[key]

			weight := int32(100)
			if backend.Weight != nil {
				weight = *backend.Weight
			}

			compiled.Backends = append(compiled.Backends, render.CompiledRouteBackend{
				AgentName: backend.AgentRef.Name,
				Namespace: ns,
				Endpoint:  status.Endpoint,
				Weight:    weight,
				Ready:     status.Ready,
			})
		}

		config.Rules = append(config.Rules, compiled)
	}

	// Sort rules by priority (descending)
	sort.Slice(config.Rules, func(i, j int) bool {
		return config.Rules[i].Priority > config.Rules[j].Priority
	})

	// Compile defaults
	if route.Spec.Defaults != nil {
		defaults := &render.RouteDefaultConfig{
			MaxConcurrent:    100,
			MaxQueueSize:     50,
			QueueTimeoutMs:   30000,
			RequestTimeoutMs: 300000,
			RejectUnmatched:  false,
		}

		if route.Spec.Defaults.CircuitBreaker != nil {
			cb := route.Spec.Defaults.CircuitBreaker
			if cb.MaxConcurrent != nil {
				defaults.MaxConcurrent = *cb.MaxConcurrent
			}
			if cb.MaxQueueSize != nil {
				defaults.MaxQueueSize = *cb.MaxQueueSize
			}
			if cb.QueueTimeout != nil {
				defaults.QueueTimeoutMs = cb.QueueTimeout.Milliseconds()
			}
			if cb.RequestTimeout != nil {
				defaults.RequestTimeoutMs = cb.RequestTimeout.Milliseconds()
			}
		}

		if route.Spec.Defaults.RejectUnmatched != nil {
			defaults.RejectUnmatched = *route.Spec.Defaults.RejectUnmatched
		}

		if route.Spec.Defaults.Backend != nil {
			ref := route.Spec.Defaults.Backend.AgentRef
			ns := ref.Namespace
			if ns == "" {
				ns = route.Namespace
			}
			key := ns + "/" + ref.Name
			status := backendMap[key]

			weight := int32(100)
			if route.Spec.Defaults.Backend.Weight != nil {
				weight = *route.Spec.Defaults.Backend.Weight
			}

			defaults.Backend = &render.CompiledRouteBackend{
				AgentName: ref.Name,
				Namespace: ns,
				Endpoint:  status.Endpoint,
				Weight:    weight,
				Ready:     status.Ready,
			}
		}

		config.Defaults = defaults
	}

	return config
}

// reconcileRoutesConfigMap creates or updates the gateway routes ConfigMap.
func (r *RouteReconciler) reconcileRoutesConfigMap(ctx context.Context, namespace string, config *render.RouteConfig) error {
	cm, err := render.GatewayRoutesConfigMap(namespace, config)
	if err != nil {
		return err
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	existing.Data = cm.Data
	existing.Labels = cm.Labels
	return r.Update(ctx, existing)
}

func (r *RouteReconciler) setCondition(route *aiv1alpha1.Route, condition metav1.Condition) {
	condition.LastTransitionTime = metav1.Now()
	meta.SetStatusCondition(&route.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RouteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1alpha1.Route{}).
		// Watch Agent resources and reconcile Routes that reference them
		Watches(
			&aiv1alpha1.Agent{},
			handler.EnqueueRequestsFromMapFunc(r.findRoutesForAgent),
		).
		Named("route").
		Complete(r)
}

// findRoutesForAgent maps an Agent to all Routes that reference it.
// This ensures Routes are reconciled when their backend Agents change status.
func (r *RouteReconciler) findRoutesForAgent(ctx context.Context, obj client.Object) []reconcile.Request {
	agent, ok := obj.(*aiv1alpha1.Agent)
	if !ok {
		return nil
	}

	logger := log.FromContext(ctx)

	// List all Routes
	var routeList aiv1alpha1.RouteList
	if err := r.List(ctx, &routeList); err != nil {
		logger.Error(err, "Failed to list Routes for Agent watch")
		return nil
	}

	// Find Routes that reference this Agent
	var requests []reconcile.Request
	for _, route := range routeList.Items {
		if r.routeReferencesAgent(&route, agent.Name, agent.Namespace) {
			logger.V(1).Info("Agent change triggers Route reconcile",
				"agent", agent.Name, "agentNamespace", agent.Namespace,
				"route", route.Name, "routeNamespace", route.Namespace)
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      route.Name,
					Namespace: route.Namespace,
				},
			})
		}
	}

	return requests
}

// routeReferencesAgent checks if a Route references a specific Agent.
func (r *RouteReconciler) routeReferencesAgent(route *aiv1alpha1.Route, agentName, agentNamespace string) bool {
	// Check rule backends
	for _, rule := range route.Spec.Rules {
		for _, backend := range rule.Backends {
			ns := backend.AgentRef.Namespace
			if ns == "" {
				ns = route.Namespace
			}
			if backend.AgentRef.Name == agentName && ns == agentNamespace {
				return true
			}
		}
	}

	// Check default backend
	if route.Spec.Defaults != nil && route.Spec.Defaults.Backend != nil {
		ref := route.Spec.Defaults.Backend.AgentRef
		ns := ref.Namespace
		if ns == "" {
			ns = route.Namespace
		}
		if ref.Name == agentName && ns == agentNamespace {
			return true
		}
	}

	return false
}
