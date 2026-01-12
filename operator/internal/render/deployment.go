package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	// DefaultAgentRunnerImage is the default strands-agent-runner image.
	DefaultAgentRunnerImage = "ghcr.io/jarsater/strands-agent-runner:latest"

	// AgentLibsImage is the shared agent libraries image (logging, etc).
	AgentLibsImage = "ghcr.io/jarsater/agent-libs:latest"

	// AgentConfigMountPath is where the agent config is mounted.
	AgentConfigMountPath = "/etc/agent/config"

	// AgentConfigFileName is the config file name.
	AgentConfigFileName = "agent.json"

	// AgentPort is the HTTP port for the agent service.
	AgentPort = 8080

	// AgentMetricsPort is the OpenTelemetry metrics port.
	AgentMetricsPort = 9090

	// GatewayNamespace is the default namespace for the agent gateway.
	GatewayNamespace = "mcp-fabric-gateway"
)

// AgentDeploymentParams holds parameters for rendering an Agent Deployment.
type AgentDeploymentParams struct {
	Agent         *aiv1alpha1.Agent
	ConfigMapName string
	ConfigHash    string
	Labels        map[string]string
	ToolPackages  []ToolPackageInfo
}

// AgentDeployment renders a Deployment for an Agent.
func AgentDeployment(params AgentDeploymentParams) *appsv1.Deployment {
	agent := params.Agent

	image := DefaultAgentRunnerImage
	if agent.Spec.Image != "" {
		image = agent.Spec.Image
	}

	replicas := int32(1)
	if agent.Spec.Replicas != nil {
		replicas = *agent.Spec.Replicas
	}

	// Selector labels (immutable, no model info)
	selectorLabels := params.Labels
	if selectorLabels == nil {
		selectorLabels = AgentLabels(agent)
	}

	// Pod labels include model metadata for Prometheus relabeling
	podLabels := AgentPodLabels(agent)

	annotations := map[string]string{
		"fabric.jarsater.ai/config-hash": params.ConfigHash,
	}

	// Build init containers for ToolPackages
	initContainers := buildToolPackageInitContainers(params.ToolPackages)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      agent.Name,
			Namespace: agent.Namespace,
			Labels:    selectorLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:           serviceAccountName(agent),
					AutomountServiceAccountToken: ptr.To(false),
					DNSPolicy:                    corev1.DNSClusterFirst,
					SecurityContext:              podSecurityContext(),
					InitContainers:               initContainers,
					Containers: []corev1.Container{
						{
							Name:            "agent",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: AgentPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: AgentMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "AGENT_CONFIG_PATH",
									Value: fmt.Sprintf("%s/%s", AgentConfigMountPath, AgentConfigFileName),
								},
								{
									Name:  "PYTHONPATH",
									Value: "/tools",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: AgentConfigMountPath,
									ReadOnly:  true,
								},
								{
									Name:      "tools",
									MountPath: "/tools",
									ReadOnly:  true,
								},
								{
									Name:      "tmp",
									MountPath: "/tmp",
								},
							},
							SecurityContext: containerSecurityContext(),
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(AgentPort),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(AgentPort),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: params.ConfigMapName,
									},
								},
							},
						},
						{
							Name: "tools",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "tmp",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					NodeSelector: agent.Spec.NodeSelector,
					Tolerations:  agent.Spec.Tolerations,
				},
			},
		},
	}

	// Apply resource requirements if specified
	if agent.Spec.Resources != nil {
		deployment.Spec.Template.Spec.Containers[0].Resources = *agent.Spec.Resources
	}

	// Add env vars from spec
	if len(agent.Spec.Env) > 0 {
		deployment.Spec.Template.Spec.Containers[0].Env = append(
			deployment.Spec.Template.Spec.Containers[0].Env,
			agent.Spec.Env...,
		)
	}

	// Add envFrom sources (for loading credentials from secrets/configmaps)
	if len(agent.Spec.EnvFrom) > 0 {
		deployment.Spec.Template.Spec.Containers[0].EnvFrom = agent.Spec.EnvFrom
	}

	return deployment
}

// podSecurityContext returns hardened pod security context.
// RunAsUser/RunAsGroup are not set, allowing each image's USER directive to take effect.
func podSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// containerSecurityContext returns hardened container security context.
func containerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		ReadOnlyRootFilesystem:   ptr.To(true),
		RunAsNonRoot:             ptr.To(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// serviceAccountName returns the SA name for an agent.
func serviceAccountName(agent *aiv1alpha1.Agent) string {
	if agent.Spec.ServiceAccountName != "" {
		return agent.Spec.ServiceAccountName
	}
	return agent.Name
}

// AgentLabels returns standard labels for an agent (used for selectors).
func AgentLabels(agent *aiv1alpha1.Agent) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       agent.Name,
		"app.kubernetes.io/component":  "agent",
		"app.kubernetes.io/managed-by": "mcp-fabric-operator",
		"fabric.jarsater.ai/agent":     agent.Name,
	}
}

// AgentPodLabels returns labels for agent pods, including model metadata.
// These labels enable Prometheus to add model info to metrics via relabeling.
func AgentPodLabels(agent *aiv1alpha1.Agent) map[string]string {
	labels := AgentLabels(agent)
	// Add model metadata labels (these are required fields in the CRD)
	labels["fabric.jarsater.ai/provider"] = agent.Spec.Model.Provider
	labels["fabric.jarsater.ai/model-id"] = sanitizeLabelValue(agent.Spec.Model.ModelID)
	// Add prompt hash to compare same prompt across different models
	labels["fabric.jarsater.ai/prompt-hash"] = HashConfig([]byte(agent.Spec.Prompt))
	return labels
}

// sanitizeLabelValue converts a string to a valid Kubernetes label value.
// Label values must be 63 chars or less, start/end with alphanumeric,
// and contain only alphanumeric, '-', '_', or '.'.
func sanitizeLabelValue(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			result = append(result, c)
		} else {
			// Replace invalid chars with underscore
			result = append(result, '_')
		}
	}
	// Truncate to 63 chars
	if len(result) > 63 {
		result = result[:63]
	}
	// Ensure starts and ends with alphanumeric
	for len(result) > 0 && !isAlphanumeric(result[0]) {
		result = result[1:]
	}
	for len(result) > 0 && !isAlphanumeric(result[len(result)-1]) {
		result = result[:len(result)-1]
	}
	if len(result) == 0 {
		return "unknown"
	}
	return string(result)
}

func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// HashConfig computes a SHA256 hash of config content for change detection.
func HashConfig(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:8])
}

// buildToolPackageInitContainers creates init containers for shared libs and each ToolPackage.
// The agent-libs init container always runs first to provide shared libraries (logging, etc).
// Each ToolPackage init container copies Python modules from its image to /tools/.
func buildToolPackageInitContainers(toolPackages []ToolPackageInfo) []corev1.Container {
	initContainers := []corev1.Container{
		// Always include agent-libs first for shared libraries
		{
			Name:            "agent-libs",
			Image:           AgentLibsImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh", "-c", "cp -r /app/* /tools/",
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "tools",
					MountPath: "/tools",
				},
			},
			SecurityContext: containerSecurityContext(),
		},
	}

	for i, tp := range toolPackages {
		initContainers = append(initContainers, corev1.Container{
			Name:            fmt.Sprintf("toolpkg-%d", i),
			Image:           tp.Image,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command: []string{
				"sh", "-c", "cp -r /app/* /tools/",
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "tools",
					MountPath: "/tools",
				},
			},
			SecurityContext: containerSecurityContext(),
		})
	}

	return initContainers
}
