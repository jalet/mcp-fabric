package render

import (
	"encoding/json"
	"fmt"
	"time"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// WorkspacePVCName returns the PVC name for a task's workspace.
func WorkspacePVCName(task *aiv1alpha1.Task) string {
	return fmt.Sprintf("%s-workspace", task.Name)
}

// OrchestratorJobParams holds parameters for rendering an orchestrator Job.
type OrchestratorJobParams struct {
	Task              *aiv1alpha1.Task
	OrchestratorAgent *aiv1alpha1.Agent
	WorkerEndpoint    string // e.g., "code-worker.mcp-fabric-agents:8080"
	WorkspacePVC      string
	PRD               string // JSON string of the PRD
}

// OrchestratorJob renders a Kubernetes Job for the task orchestrator.
// The Job includes an optional git-clone init container when GitConfig is present.
func OrchestratorJob(params OrchestratorJobParams) (*batchv1.Job, error) {
	task := params.Task
	agent := params.OrchestratorAgent

	// Get image from orchestrator agent
	image := agent.Spec.Image
	if image == "" {
		return nil, fmt.Errorf("orchestrator agent %s has no image specified", agent.Name)
	}

	// Build the task config to pass to the orchestrator
	taskConfig := map[string]interface{}{
		"taskName":       task.Name,
		"prd":            json.RawMessage(params.PRD),
		"workerEndpoint": params.WorkerEndpoint,
		"context":        task.Spec.Context,
	}

	// Add quality gates if configured
	if len(task.Spec.QualityGates) > 0 {
		taskConfig["qualityGates"] = task.Spec.QualityGates
	}

	// Add limits if configured
	if task.Spec.Limits != nil {
		limitsMap := map[string]interface{}{}
		if task.Spec.Limits.MaxIterations != nil {
			limitsMap["maxIterations"] = *task.Spec.Limits.MaxIterations
		}
		if task.Spec.Limits.IterationTimeout != nil {
			limitsMap["iterationTimeout"] = task.Spec.Limits.IterationTimeout.Duration.String()
		}
		if task.Spec.Limits.MaxConsecutiveFailures != nil {
			limitsMap["maxConsecutiveFailures"] = *task.Spec.Limits.MaxConsecutiveFailures
		}
		taskConfig["limits"] = limitsMap
	}

	// Add git config if present (for finalization)
	if task.Spec.Git != nil {
		gitConfigMap := map[string]interface{}{
			"url":          task.Spec.Git.URL,
			"branch":       getStringOrDefault(task.Spec.Git.Branch, "main"),
			"baseBranch":   task.Spec.Git.BaseBranch,
			"commitAuthor": getStringOrDefault(task.Spec.Git.CommitAuthor, "MCP Fabric Task"),
			"commitEmail":  getStringOrDefault(task.Spec.Git.CommitEmail, "task@mcp-fabric.local"),
			"autoPush":     getBoolOrDefault(task.Spec.Git.AutoPush, true),
			"createPR":     getBoolOrDefault(task.Spec.Git.CreatePR, true),
			"draftPR":      getBoolOrDefault(task.Spec.Git.DraftPR, true),
			"prTitle":      task.Spec.Git.PRTitle,
			"prBody":       task.Spec.Git.PRBody,
			"provider":     string(task.Spec.Git.Provider),
		}
		taskConfig["git"] = gitConfigMap
	}

	taskJSON, err := json.Marshal(taskConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task config: %w", err)
	}

	// Calculate timeout - use total timeout for orchestrator job
	timeout := 24 * time.Hour // default
	if task.Spec.Limits != nil && task.Spec.Limits.TotalTimeout != nil {
		timeout = task.Spec.Limits.TotalTimeout.Duration
	}

	jobName := fmt.Sprintf("%s-orchestrator", task.Name)
	if len(jobName) > 63 {
		jobName = jobName[:63]
	}

	labels := OrchestratorJobLabels(task)

	// Build volumes
	volumes := []corev1.Volume{
		{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: params.WorkspacePVC,
				},
			},
		},
		{
			Name: "tmp",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// Add git-home volume for git credentials if git is configured
	if task.Spec.Git != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "git-home",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		// Mount the credentials secret as a file for secure token access
		volumes = append(volumes, corev1.Volume{
			Name: "git-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: task.Spec.Git.CredentialsSecret.Name,
					Items: []corev1.KeyToPath{
						{
							Key:  "token",
							Path: "token",
							Mode: ptr.To(int32(0400)), // Read-only for owner
						},
					},
				},
			},
		})
	}

	// Build init containers
	var initContainers []corev1.Container
	if task.Spec.Git != nil {
		initContainers = append(initContainers, gitCloneInitContainer(task.Spec.Git))
	}

	// Build orchestrator container
	orchestratorContainer := corev1.Container{
		Name:            "orchestrator",
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name:  "TASK_CONFIG",
				Value: string(taskJSON),
			},
			{
				Name:  "WORKSPACE_DIR",
				Value: "/workspace",
			},
			{
				Name:  "PYTHONUNBUFFERED",
				Value: "1",
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: "/workspace",
			},
			{
				Name:      "tmp",
				MountPath: "/tmp",
			},
		},
		SecurityContext: containerSecurityContext(),
	}

	// Add git-related volume mounts if git is configured
	if task.Spec.Git != nil {
		orchestratorContainer.VolumeMounts = append(orchestratorContainer.VolumeMounts,
			corev1.VolumeMount{
				Name:      "git-home",
				MountPath: "/home/appuser",
			},
			corev1.VolumeMount{
				Name:      "git-credentials",
				MountPath: "/secrets/git",
				ReadOnly:  true,
			},
		)
		// Tell orchestrator where to find the git token file
		// The orchestrator should read from /secrets/git/token instead of env var
		orchestratorContainer.Env = append(orchestratorContainer.Env, corev1.EnvVar{
			Name:  "GIT_TOKEN_FILE",
			Value: "/secrets/git/token",
		})
	}

	// Add env vars from orchestrator agent spec
	if len(agent.Spec.Env) > 0 {
		orchestratorContainer.Env = append(orchestratorContainer.Env, agent.Spec.Env...)
	}

	// Add envFrom sources
	if len(agent.Spec.EnvFrom) > 0 {
		orchestratorContainer.EnvFrom = agent.Spec.EnvFrom
	}

	// Apply resource requirements
	if agent.Spec.Resources != nil {
		orchestratorContainer.Resources = *agent.Spec.Resources
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: task.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)), // No retries - we handle failure in orchestrator
			ActiveDeadlineSeconds:   ptr.To(int64(timeout.Seconds())),
			TTLSecondsAfterFinished: ptr.To(int32(3600)), // Cleanup after 1 hour (longer for debugging)
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy:                corev1.RestartPolicyNever,
					AutomountServiceAccountToken: ptr.To(false),
					SecurityContext:              podSecurityContext(),
					InitContainers:               initContainers,
					Containers:                   []corev1.Container{orchestratorContainer},
					Volumes:                      volumes,
					NodeSelector:                 agent.Spec.NodeSelector,
					Tolerations:                  agent.Spec.Tolerations,
				},
			},
		},
	}

	return job, nil
}

// OrchestratorJobLabels returns labels for an orchestrator Job.
func OrchestratorJobLabels(task *aiv1alpha1.Task) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       fmt.Sprintf("%s-orchestrator", task.Name),
		"app.kubernetes.io/component":  "task-orchestrator",
		"app.kubernetes.io/managed-by": "mcp-fabric-operator",
		"fabric.jarsater.ai/task":      task.Name,
	}
}

// DefaultGitImage is the default container image for git operations.
const DefaultGitImage = "alpine/git:2.43"

// gitCloneInitContainer creates an init container that clones a git repository.
// The git token is read from a mounted secret file for security (not from env vars).
func gitCloneInitContainer(gitConfig *aiv1alpha1.GitConfig) corev1.Container {
	// Build the clone script with feature branch support
	// Token is read from mounted secret file to avoid exposure in env vars or logs
	script := `
set -e
echo "Configuring git credentials..."
mkdir -p /home/appuser

# Read token from mounted secret (more secure than env var)
GIT_TOKEN=$(cat /secrets/git/token)

git config --global credential.helper store
echo "https://x-access-token:${GIT_TOKEN}@github.com" > /home/appuser/.git-credentials
chmod 600 /home/appuser/.git-credentials
git config --global user.name "${GIT_AUTHOR}"
git config --global user.email "${GIT_EMAIL}"
git config --global --add safe.directory /workspace

echo "Cloning repository..."
if [ "${GIT_DEPTH}" = "0" ]; then
    git clone "${GIT_URL}" /workspace
else
    git clone --depth "${GIT_DEPTH}" "${GIT_URL}" /workspace
fi

cd /workspace

# If BaseBranch is set, create feature branch from it
if [ -n "${GIT_BASE_BRANCH}" ]; then
    echo "Creating feature branch ${GIT_BRANCH} from ${GIT_BASE_BRANCH}..."
    git fetch origin "${GIT_BASE_BRANCH}"
    git checkout -b "${GIT_BRANCH}" "origin/${GIT_BASE_BRANCH}"
else
    echo "Checking out branch ${GIT_BRANCH}..."
    git checkout "${GIT_BRANCH}" 2>/dev/null || git checkout -b "${GIT_BRANCH}"
fi

echo "Git setup complete. HEAD: $(git rev-parse HEAD)"
`

	depth := int32(1)
	if gitConfig.Depth != nil {
		depth = *gitConfig.Depth
	}

	// Use configured image or default
	gitImage := DefaultGitImage
	if gitConfig.Image != "" {
		gitImage = gitConfig.Image
	}

	return corev1.Container{
		Name:    "git-clone",
		Image:   gitImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		Env: []corev1.EnvVar{
			// Non-sensitive git configuration (safe to expose)
			{Name: "GIT_URL", Value: gitConfig.URL},
			{Name: "GIT_BRANCH", Value: getStringOrDefault(gitConfig.Branch, "main")},
			{Name: "GIT_BASE_BRANCH", Value: gitConfig.BaseBranch},
			{Name: "GIT_DEPTH", Value: fmt.Sprintf("%d", depth)},
			{Name: "GIT_AUTHOR", Value: getStringOrDefault(gitConfig.CommitAuthor, "MCP Fabric Task")},
			{Name: "GIT_EMAIL", Value: getStringOrDefault(gitConfig.CommitEmail, "task@mcp-fabric.local")},
			// Note: GIT_TOKEN is read from mounted secret file, not env var
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: "/workspace"},
			{Name: "git-home", MountPath: "/home/appuser"},
			{Name: "git-credentials", MountPath: "/secrets/git", ReadOnly: true},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			RunAsNonRoot:             ptr.To(false), // Git image runs as root by default
			ReadOnlyRootFilesystem:   ptr.To(false), // Needs to write .git-credentials
		},
	}
}

// Helper functions for default values

func getStringOrDefault(s string, defaultVal string) string {
	if s == "" {
		return defaultVal
	}
	return s
}

func getBoolOrDefault(b *bool, defaultVal bool) bool {
	if b == nil {
		return defaultVal
	}
	return *b
}

