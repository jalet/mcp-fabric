package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
)

// AgentService renders a ClusterIP Service for an Agent.
func AgentService(agent *aiv1alpha1.Agent, labels map[string]string) *corev1.Service {
	if labels == nil {
		labels = AgentLabels(agent)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       AgentPort,
					TargetPort: intstr.FromInt32(AgentPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// AgentEndpoint returns the fully qualified service endpoint for an agent.
func AgentEndpoint(agent *aiv1alpha1.Agent) string {
	return agent.Name + "." + agent.Namespace + ".svc.cluster.local:" + "8080"
}
