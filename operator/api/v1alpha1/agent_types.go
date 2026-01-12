package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ModelConfig defines the LLM configuration for the agent.
type ModelConfig struct {
	// Provider is the model provider (e.g., "anthropic", "openai", "bedrock").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// ModelID is the model identifier (e.g., "claude-sonnet-4-20250514").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ModelID string `json:"modelId"`

	// Temperature controls randomness (0.0-1.0).
	// +optional
	Temperature *float64 `json:"temperature,omitempty"`

	// MaxTokens limits response length.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxTokens *int32 `json:"maxTokens,omitempty"`

	// Endpoint overrides the default provider endpoint.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
}

// ToolRef references a Tool and optionally selects specific tools.
type ToolRef struct {
	// Name of the Tool.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Tool (defaults to agent namespace).
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// EnabledTools lists specific tools to enable (empty means all).
	// +optional
	EnabledTools []string `json:"enabledTools,omitempty"`

	// DisabledTools lists specific tools to disable.
	// +optional
	DisabledTools []string `json:"disabledTools,omitempty"`
}

// MCPServerSelector selects MCPServer resources by label.
type MCPServerSelector struct {
	// LabelSelector matches MCPServer resources.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Namespaces to search for MCPServers (empty means agent namespace only).
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`
}

// AgentPolicy defines runtime constraints for the agent.
type AgentPolicy struct {
	// MaxToolCalls limits total tool invocations per request.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=50
	// +optional
	MaxToolCalls *int32 `json:"maxToolCalls,omitempty"`

	// RequestTimeout is the maximum duration for a single request.
	// +kubebuilder:default="5m"
	// +optional
	RequestTimeout *metav1.Duration `json:"requestTimeout,omitempty"`

	// ToolTimeout is the maximum duration for a single tool call.
	// +kubebuilder:default="30s"
	// +optional
	ToolTimeout *metav1.Duration `json:"toolTimeout,omitempty"`

	// MaxConcurrentRequests limits parallel request processing.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=10
	// +optional
	MaxConcurrentRequests *int32 `json:"maxConcurrentRequests,omitempty"`
}

// AgentTool declares an MCP tool exposed by this agent.
type AgentTool struct {
	// Name is the tool identifier (e.g., "analyze_costs").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Description explains what the tool does.
	// +kubebuilder:validation:Required
	Description string `json:"description"`

	// InputSchema is the JSON Schema for tool parameters.
	// +optional
	InputSchema *apiextensionsv1.JSON `json:"inputSchema,omitempty"`
}

// NetworkSpec defines network egress rules for the agent.
type NetworkSpec struct {
	// AllowedFQDNs lists FQDNs the agent can connect to.
	// +optional
	AllowedFQDNs []string `json:"allowedFqdns,omitempty"`

	// AllowedCIDRs lists CIDR blocks the agent can connect to.
	// +optional
	AllowedCIDRs []string `json:"allowedCidrs,omitempty"`

	// AllowModelProvider automatically allows egress to the model provider endpoint.
	// +kubebuilder:default=true
	// +optional
	AllowModelProvider *bool `json:"allowModelProvider,omitempty"`

	// AllowObjectStore automatically allows egress to the configured object store.
	// +kubebuilder:default=false
	// +optional
	AllowObjectStore *bool `json:"allowObjectStore,omitempty"`
}

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	// Prompt is the system instruction/persona for the agent.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Prompt string `json:"prompt"`

	// Model configures the LLM backend.
	// +kubebuilder:validation:Required
	Model ModelConfig `json:"model"`

	// ToolPackages references Tool resources providing tools.
	// +optional
	ToolPackages []ToolRef `json:"toolPackages,omitempty"`

	// MCPSelector selects MCPServer resources to connect to.
	// +optional
	MCPSelector *MCPServerSelector `json:"mcpSelector,omitempty"`

	// Policy defines runtime constraints.
	// +optional
	Policy *AgentPolicy `json:"policy,omitempty"`

	// Network defines egress rules.
	// +optional
	Network *NetworkSpec `json:"network,omitempty"`

	// Replicas is the number of agent pods.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources defines compute resource requirements.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Image overrides the default strands-agent-runner image.
	// +optional
	Image string `json:"image,omitempty"`

	// ServiceAccountName to use for the agent pods.
	// If not set, a minimal SA is created.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// NodeSelector for pod scheduling.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for pod scheduling.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Env sets environment variables directly in the agent container.
	// Use for non-secret values like AWS_DEFAULT_REGION.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom sources environment variables from Secrets or ConfigMaps.
	// Use this to inject credentials (e.g., AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY).
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Tools declares MCP tools this agent exposes.
	// These are used by the gateway for MCP protocol discovery.
	// +optional
	Tools []AgentTool `json:"tools,omitempty"`
}

// ResolvedMCPEndpoint represents a discovered MCP server endpoint.
type ResolvedMCPEndpoint struct {
	// Name of the MCPServer resource.
	Name string `json:"name"`

	// Namespace of the MCPServer resource.
	Namespace string `json:"namespace"`

	// Endpoint is the resolved service URL.
	Endpoint string `json:"endpoint"`

	// Ready indicates if the MCPServer is ready.
	Ready bool `json:"ready"`
}

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	// Ready indicates the agent deployment is ready to serve requests.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// ObservedGeneration is the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Endpoint is the agent service endpoint (service.namespace.svc.cluster.local:port).
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// AvailableReplicas is the number of ready pods.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// ResolvedMCPEndpoints lists discovered MCP server endpoints.
	// +optional
	ResolvedMCPEndpoints []ResolvedMCPEndpoint `json:"resolvedMcpEndpoints,omitempty"`

	// ConfigHash is the hash of the current agent configuration.
	// Used to trigger rolling updates.
	// +optional
	ConfigHash string `json:"configHash,omitempty"`

	// AvailableTools lists the MCP tools ready for discovery.
	// Populated from spec.tools when agent becomes ready.
	// +optional
	AvailableTools []AgentTool `json:"availableTools,omitempty"`

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
// +kubebuilder:resource:shortName=ag
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Agent ready"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.availableReplicas",description="Available replicas"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".status.endpoint",description="Service endpoint"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Agent declares a Strands AI agent with tools and MCP connections.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AgentSpec   `json:"spec,omitempty"`
	Status AgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Agent{}, &AgentList{})
}
