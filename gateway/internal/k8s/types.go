package k8s

// Agent represents a simplified Agent CRD for the gateway.
type Agent struct {
	Name      string
	Namespace string
	Spec      AgentSpec
	Status    AgentStatus
}

// AgentSpec contains the agent specification.
type AgentSpec struct {
	Prompt string
	Tools  []AgentTool
}

// AgentTool declares an MCP tool exposed by an agent.
type AgentTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// AgentStatus contains the agent status.
type AgentStatus struct {
	Ready          bool
	Endpoint       string
	AvailableTools []AgentTool
}
