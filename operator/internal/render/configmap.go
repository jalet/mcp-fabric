package render

import (
	"encoding/json"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AgentConfig is the runtime configuration passed to the strands-agent-runner.
type AgentConfig struct {
	// Prompt is the system instruction.
	Prompt string `json:"prompt"`

	// Model configuration.
	Model AgentModelConfig `json:"model"`

	// ToolPackages lists tool packages and enabled tools.
	ToolPackages []AgentToolPackageConfig `json:"toolPackages,omitempty"`

	// MCPEndpoints lists MCP server endpoints to connect to.
	MCPEndpoints []AgentMCPEndpoint `json:"mcpEndpoints,omitempty"`

	// Policy defines runtime constraints.
	Policy AgentPolicyConfig `json:"policy"`
}

// AgentModelConfig is the model configuration in the agent config.
type AgentModelConfig struct {
	Provider    string   `json:"provider"`
	ModelID     string   `json:"modelId"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int32   `json:"maxTokens,omitempty"`
	Endpoint    string   `json:"endpoint,omitempty"`
}

// AgentToolPackageConfig references a tool package in the agent config.
type AgentToolPackageConfig struct {
	Name          string   `json:"name"`
	Namespace     string   `json:"namespace"`
	Image         string   `json:"image"`
	EntryModule   string   `json:"entryModule,omitempty"`
	EnabledTools  []string `json:"enabledTools,omitempty"`
	DisabledTools []string `json:"disabledTools,omitempty"`
}

// AgentMCPEndpoint represents a resolved MCP server endpoint.
type AgentMCPEndpoint struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Endpoint  string `json:"endpoint"`
}

// AgentPolicyConfig contains runtime policy in the agent config.
type AgentPolicyConfig struct {
	MaxToolCalls          int32 `json:"maxToolCalls"`
	RequestTimeoutSeconds int32 `json:"requestTimeoutSeconds"`
	ToolTimeoutSeconds    int32 `json:"toolTimeoutSeconds"`
	MaxConcurrentRequests int32 `json:"maxConcurrentRequests"`
}

// AgentConfigMapParams holds parameters for rendering an Agent ConfigMap.
type AgentConfigMapParams struct {
	Agent        *aiv1alpha1.Agent
	ToolPackages []ToolPackageInfo
	MCPEndpoints []AgentMCPEndpoint
	Labels       map[string]string
}

// ToolPackageInfo holds resolved info about a ToolPackage.
type ToolPackageInfo struct {
	Name          string
	Namespace     string
	Image         string
	EntryModule   string
	EnabledTools  []string
	DisabledTools []string
}

// AgentConfigMap renders a ConfigMap containing the agent runtime configuration.
func AgentConfigMap(params AgentConfigMapParams) (*corev1.ConfigMap, []byte, error) {
	agent := params.Agent
	labels := params.Labels
	if labels == nil {
		labels = AgentLabels(agent)
	}

	// Build the config
	config := AgentConfig{
		Prompt: agent.Spec.Prompt,
		Model: AgentModelConfig{
			Provider:    agent.Spec.Model.Provider,
			ModelID:     agent.Spec.Model.ModelID,
			Temperature: agent.Spec.Model.Temperature,
			MaxTokens:   agent.Spec.Model.MaxTokens,
			Endpoint:    agent.Spec.Model.Endpoint,
		},
		MCPEndpoints: params.MCPEndpoints,
		Policy:       buildPolicyConfig(agent.Spec.Policy),
	}

	// Add tool packages
	for _, tp := range params.ToolPackages {
		config.ToolPackages = append(config.ToolPackages, AgentToolPackageConfig(tp))
	}

	// Marshal to JSON
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name + "-config",
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			AgentConfigFileName: string(configJSON),
		},
	}

	return cm, configJSON, nil
}

func buildPolicyConfig(policy *aiv1alpha1.AgentPolicy) AgentPolicyConfig {
	cfg := AgentPolicyConfig{
		MaxToolCalls:          50,
		RequestTimeoutSeconds: 300, // 5 minutes
		ToolTimeoutSeconds:    30,
		MaxConcurrentRequests: 10,
	}

	if policy == nil {
		return cfg
	}

	if policy.MaxToolCalls != nil {
		cfg.MaxToolCalls = *policy.MaxToolCalls
	}
	if policy.RequestTimeout != nil {
		cfg.RequestTimeoutSeconds = int32(policy.RequestTimeout.Seconds())
	}
	if policy.ToolTimeout != nil {
		cfg.ToolTimeoutSeconds = int32(policy.ToolTimeout.Seconds())
	}
	if policy.MaxConcurrentRequests != nil {
		cfg.MaxConcurrentRequests = *policy.MaxConcurrentRequests
	}

	return cfg
}

// RouteConfig is the compiled routing configuration for the gateway.
type RouteConfig struct {
	Rules    []CompiledRouteRule `json:"rules"`
	Defaults *RouteDefaultConfig `json:"defaults,omitempty"`
}

// CompiledRouteRule is a pre-compiled route rule for the gateway.
type CompiledRouteRule struct {
	Name     string                 `json:"name"`
	Priority int32                  `json:"priority"`
	Match    CompiledRouteMatch     `json:"match"`
	Backends []CompiledRouteBackend `json:"backends"`
}

// CompiledRouteMatch is the match criteria for a compiled rule.
type CompiledRouteMatch struct {
	Agent       string            `json:"agent,omitempty"`
	IntentRegex string            `json:"intentRegex,omitempty"`
	TenantID    string            `json:"tenantId,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// CompiledRouteBackend is a resolved backend in a compiled rule.
type CompiledRouteBackend struct {
	AgentName string `json:"agentName"`
	Namespace string `json:"namespace"`
	Endpoint  string `json:"endpoint"`
	Weight    int32  `json:"weight"`
	Ready     bool   `json:"ready"`
}

// RouteDefaultConfig contains default routing configuration.
type RouteDefaultConfig struct {
	Backend          *CompiledRouteBackend `json:"backend,omitempty"`
	MaxConcurrent    int32                 `json:"maxConcurrent"`
	MaxQueueSize     int32                 `json:"maxQueueSize"`
	QueueTimeoutMs   int64                 `json:"queueTimeoutMs"`
	RequestTimeoutMs int64                 `json:"requestTimeoutMs"`
	RejectUnmatched  bool                  `json:"rejectUnmatched"`
}

// GatewayRoutesConfigMap renders the ConfigMap consumed by the agent gateway.
func GatewayRoutesConfigMap(namespace string, config *RouteConfig) (*corev1.ConfigMap, error) {
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mcp-fabric-gateway-routes",
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "mcp-fabric-gateway",
				"app.kubernetes.io/component":  "routes",
				"app.kubernetes.io/managed-by": "mcp-fabric-operator",
			},
		},
		Data: map[string]string{
			"routes.json": string(configJSON),
		},
	}, nil
}
