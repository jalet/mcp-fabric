package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

var agentGVR = schema.GroupVersionResource{
	Group:    "fabric.jarsater.ai",
	Version:  "v1alpha1",
	Resource: "agents",
}

// AgentWatcher watches Agent CRDs and maintains an in-memory cache.
type AgentWatcher struct {
	logger    *zap.SugaredLogger
	client    dynamic.Interface
	informer  cache.SharedIndexInformer
	agents    sync.Map // name -> *Agent
	onChange  func()   // callback when agents change
	namespace string   // empty for all namespaces
}

// NewAgentWatcher creates a new watcher for Agent CRDs.
func NewAgentWatcher(logger *zap.SugaredLogger, namespace string, onChange func()) (*AgentWatcher, error) {
	config, err := getKubeConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubeconfig: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &AgentWatcher{
		logger:    logger,
		client:    client,
		namespace: namespace,
		onChange:  onChange,
	}, nil
}

// getKubeConfig returns the Kubernetes client configuration.
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	return kubeConfig.ClientConfig()
}

// Start begins watching Agent CRDs.
func (w *AgentWatcher) Start(ctx context.Context) error {
	// Create informer factory
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		w.client,
		30*time.Second, // resync period
		w.namespace,
		nil,
	)

	w.informer = factory.ForResource(agentGVR).Informer()

	// Add event handlers
	_, _ = w.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.onAdd,
		UpdateFunc: w.onUpdate,
		DeleteFunc: w.onDelete,
	})

	// Start informer
	w.logger.Infof("Starting Agent CRD watcher (namespace=%q)", w.namespace)
	go w.informer.Run(ctx.Done())

	// Wait for initial sync
	if !cache.WaitForCacheSync(ctx.Done(), w.informer.HasSynced) {
		return fmt.Errorf("failed to sync agent cache")
	}

	w.logger.Info("Agent CRD watcher synced")
	return nil
}

func (w *AgentWatcher) onAdd(obj interface{}) {
	agent := w.unstructuredToAgent(obj.(*unstructured.Unstructured))
	if agent == nil {
		return
	}

	w.logger.Infof("Agent added: %s/%s (ready=%v)", agent.Namespace, agent.Name, agent.Status.Ready)
	w.agents.Store(w.agentKey(agent), agent)

	if w.onChange != nil {
		w.onChange()
	}
}

func (w *AgentWatcher) onUpdate(oldObj, newObj interface{}) {
	agent := w.unstructuredToAgent(newObj.(*unstructured.Unstructured))
	if agent == nil {
		return
	}

	w.logger.Debugf("Agent updated: %s/%s (ready=%v)", agent.Namespace, agent.Name, agent.Status.Ready)
	w.agents.Store(w.agentKey(agent), agent)

	if w.onChange != nil {
		w.onChange()
	}
}

func (w *AgentWatcher) onDelete(obj interface{}) {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		// Handle DeletedFinalStateUnknown
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		u, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	}

	key := u.GetNamespace() + "/" + u.GetName()
	w.logger.Infof("Agent deleted: %s", key)
	w.agents.Delete(key)

	if w.onChange != nil {
		w.onChange()
	}
}

func (w *AgentWatcher) agentKey(agent *Agent) string {
	return agent.Namespace + "/" + agent.Name
}

func (w *AgentWatcher) unstructuredToAgent(u *unstructured.Unstructured) *Agent {
	agent := &Agent{
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
	}

	// Extract spec
	spec, found, err := unstructured.NestedMap(u.Object, "spec")
	if err != nil || !found {
		return agent
	}

	// Get prompt
	if prompt, ok := spec["prompt"].(string); ok {
		agent.Spec.Prompt = prompt
	}

	// Get tools
	if tools, ok := spec["tools"].([]interface{}); ok {
		for _, t := range tools {
			if toolMap, ok := t.(map[string]interface{}); ok {
				tool := AgentTool{
					Name:        getString(toolMap, "name"),
					Description: getString(toolMap, "description"),
				}
				if schema, ok := toolMap["inputSchema"].(map[string]interface{}); ok {
					tool.InputSchema = schema
				}
				agent.Spec.Tools = append(agent.Spec.Tools, tool)
			}
		}
	}

	// Extract status
	status, found, err := unstructured.NestedMap(u.Object, "status")
	if err != nil || !found {
		return agent
	}

	// Get ready
	if ready, ok := status["ready"].(bool); ok {
		agent.Status.Ready = ready
	}

	// Get endpoint
	if endpoint, ok := status["endpoint"].(string); ok {
		agent.Status.Endpoint = endpoint
	}

	// Get available tools
	if tools, ok := status["availableTools"].([]interface{}); ok {
		for _, t := range tools {
			if toolMap, ok := t.(map[string]interface{}); ok {
				tool := AgentTool{
					Name:        getString(toolMap, "name"),
					Description: getString(toolMap, "description"),
				}
				if schema, ok := toolMap["inputSchema"].(map[string]interface{}); ok {
					tool.InputSchema = schema
				}
				agent.Status.AvailableTools = append(agent.Status.AvailableTools, tool)
			}
		}
	}

	return agent
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// List returns all cached agents.
func (w *AgentWatcher) List() []*Agent {
	var agents []*Agent
	w.agents.Range(func(key, value interface{}) bool {
		if agent, ok := value.(*Agent); ok {
			agents = append(agents, agent)
		}
		return true
	})
	return agents
}

// ListReady returns only ready agents.
func (w *AgentWatcher) ListReady() []*Agent {
	var agents []*Agent
	w.agents.Range(func(key, value interface{}) bool {
		if agent, ok := value.(*Agent); ok && agent.Status.Ready {
			agents = append(agents, agent)
		}
		return true
	})
	return agents
}

// Get returns an agent by namespace/name.
func (w *AgentWatcher) Get(namespace, name string) (*Agent, bool) {
	key := namespace + "/" + name
	if value, ok := w.agents.Load(key); ok {
		return value.(*Agent), true
	}
	return nil, false
}

// GetByName returns an agent by name (first match).
func (w *AgentWatcher) GetByName(name string) (*Agent, bool) {
	var found *Agent
	w.agents.Range(func(key, value interface{}) bool {
		if agent, ok := value.(*Agent); ok && agent.Name == name {
			found = agent
			return false // stop iteration
		}
		return true
	})
	return found, found != nil
}

// ToJSON returns the agent list as JSON (for debugging).
func (w *AgentWatcher) ToJSON() ([]byte, error) {
	agents := w.List()
	return json.MarshalIndent(agents, "", "  ")
}

// FetchAgents does a one-time list of agents (useful for initial load).
func (w *AgentWatcher) FetchAgents(ctx context.Context) error {
	list, err := w.client.Resource(agentGVR).Namespace(w.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list agents: %w", err)
	}

	for _, item := range list.Items {
		agent := w.unstructuredToAgent(&item)
		if agent != nil {
			w.agents.Store(w.agentKey(agent), agent)
		}
	}

	w.logger.Infof("Fetched %d agents", len(list.Items))
	return nil
}
