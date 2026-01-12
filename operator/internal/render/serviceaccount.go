package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
)

// AgentServiceAccount renders a minimal ServiceAccount for an Agent.
// By default, agents get no special permissions.
func AgentServiceAccount(agent *aiv1alpha1.Agent, labels map[string]string) *corev1.ServiceAccount {
	if labels == nil {
		labels = AgentLabels(agent)
	}

	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		// No secrets, no imagePullSecrets by default
		// Pods should set automountServiceAccountToken: false
	}
}
