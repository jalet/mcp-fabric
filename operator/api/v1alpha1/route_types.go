package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RouteBackend defines a target agent for routing.
type RouteBackend struct {
	// AgentRef references an Agent by name.
	// +kubebuilder:validation:Required
	AgentRef AgentRef `json:"agentRef"`

	// Weight determines selection probability (0-100).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	// +optional
	Weight *int32 `json:"weight,omitempty"`
}

// AgentRef references an Agent resource.
type AgentRef struct {
	// Name of the Agent.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Agent (defaults to route namespace).
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RouteRule defines a single routing rule.
type RouteRule struct {
	// Name is a unique identifier for this rule.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Priority determines rule evaluation order (higher = first).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	// +optional
	Priority *int32 `json:"priority,omitempty"`

	// Match defines conditions for this rule.
	// +kubebuilder:validation:Required
	Match RouteMatch `json:"match"`

	// Backends are the target agents (supports weighted routing).
	// +kubebuilder:validation:MinItems=1
	Backends []RouteBackend `json:"backends"`
}

// RouteMatch defines matching criteria for a route rule.
type RouteMatch struct {
	// Agent matches requests with explicit agent name.
	// +optional
	Agent string `json:"agent,omitempty"`

	// IntentRegex matches the request intent field.
	// Uses RE2 regex syntax.
	// +optional
	IntentRegex string `json:"intentRegex,omitempty"`

	// TenantID matches requests from a specific tenant.
	// +optional
	TenantID string `json:"tenantId,omitempty"`

	// Headers matches request metadata headers.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`
}

// CircuitBreakerConfig defines circuit breaker settings.
type CircuitBreakerConfig struct {
	// MaxConcurrent limits concurrent requests to backends.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=100
	// +optional
	MaxConcurrent *int32 `json:"maxConcurrent,omitempty"`

	// MaxQueueSize limits queued requests when at capacity.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=50
	// +optional
	MaxQueueSize *int32 `json:"maxQueueSize,omitempty"`

	// QueueTimeout is how long requests wait in queue.
	// +kubebuilder:default="30s"
	// +optional
	QueueTimeout *metav1.Duration `json:"queueTimeout,omitempty"`

	// RequestTimeout is the maximum backend request duration.
	// +kubebuilder:default="5m"
	// +optional
	RequestTimeout *metav1.Duration `json:"requestTimeout,omitempty"`
}

// RouteDefaults defines default behavior when no rules match.
type RouteDefaults struct {
	// Backend is the fallback agent when no rules match.
	// +optional
	Backend *RouteBackend `json:"backend,omitempty"`

	// CircuitBreaker configures request limiting.
	// +optional
	CircuitBreaker *CircuitBreakerConfig `json:"circuitBreaker,omitempty"`

	// RejectUnmatched returns an error for unmatched requests.
	// If false and no default backend, returns 404.
	// +kubebuilder:default=false
	// +optional
	RejectUnmatched *bool `json:"rejectUnmatched,omitempty"`
}

// RouteSpec defines the desired state of Route.
type RouteSpec struct {
	// Rules define routing conditions and backends.
	// +optional
	Rules []RouteRule `json:"rules,omitempty"`

	// Defaults configure fallback behavior.
	// +optional
	Defaults *RouteDefaults `json:"defaults,omitempty"`

	// GatewaySelector identifies which gateway consumes these routes.
	// +optional
	GatewaySelector map[string]string `json:"gatewaySelector,omitempty"`
}

// BackendStatus represents the health of a backend agent.
type BackendStatus struct {
	// AgentRef identifies the agent.
	AgentRef AgentRef `json:"agentRef"`

	// Ready indicates the agent is available.
	Ready bool `json:"ready"`

	// Endpoint is the resolved agent service URL.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// RouteStatus defines the observed state of Route.
type RouteStatus struct {
	// Ready indicates all referenced agents are available.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// ObservedGeneration is the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ActiveRules is the count of compiled rules.
	// +optional
	ActiveRules int32 `json:"activeRules,omitempty"`

	// Backends lists the health of all referenced agents.
	// +optional
	Backends []BackendStatus `json:"backends,omitempty"`

	// CompiledConfigMap is the name of the generated routes ConfigMap.
	// +optional
	CompiledConfigMap string `json:"compiledConfigMap,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rt
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="All backends ready"
// +kubebuilder:printcolumn:name="Rules",type="integer",JSONPath=".status.activeRules",description="Active rules"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Route declares routing rules from the gateway to agents.
type Route struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RouteSpec   `json:"spec,omitempty"`
	Status RouteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RouteList contains a list of Route.
type RouteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Route `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Route{}, &RouteList{})
}
