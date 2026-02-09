package render

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestOrchestratorJob(t *testing.T) {
	tests := []struct {
		name        string
		params      OrchestratorJobParams
		wantErr     bool
		errContains string
		validate    func(t *testing.T, job *batchv1.Job)
	}{
		{
			name: "basic orchestrator job without git",
			params: OrchestratorJobParams{
				Task: &aiv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task",
						Namespace: "default",
					},
					Spec: aiv1alpha1.TaskSpec{
						Context: "Build a new feature",
					},
				},
				OrchestratorAgent: &aiv1alpha1.Agent{
					Spec: aiv1alpha1.AgentSpec{Image: "orchestrator:v1"},
				},
				WorkerEndpoint: "code-worker.default:8080",
				WorkspacePVC:   "test-workspace",
				PRD:            `{"tasks":[{"id":"1","title":"Task 1"}]}`,
			},
			wantErr: false,
			validate: func(t *testing.T, job *batchv1.Job) {
				if job.Name != "test-task-orchestrator" {
					t.Errorf("expected job name 'test-task-orchestrator', got %s", job.Name)
				}
				// Should have no init containers without git
				if len(job.Spec.Template.Spec.InitContainers) != 0 {
					t.Errorf("expected 0 init containers, got %d", len(job.Spec.Template.Spec.InitContainers))
				}
				// Check TASK_CONFIG env var
				found := false
				for _, env := range job.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "TASK_CONFIG" {
						found = true
						var config map[string]interface{}
						if err := json.Unmarshal([]byte(env.Value), &config); err != nil {
							t.Errorf("TASK_CONFIG is not valid JSON: %v", err)
						}
						if config["workerEndpoint"] != "code-worker.default:8080" {
							t.Errorf("expected workerEndpoint 'code-worker.default:8080', got %v", config["workerEndpoint"])
						}
					}
				}
				if !found {
					t.Error("TASK_CONFIG env var not found")
				}
			},
		},
		{
			name: "orchestrator job with git config",
			params: OrchestratorJobParams{
				Task: &aiv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task",
						Namespace: "default",
					},
					Spec: aiv1alpha1.TaskSpec{
						Git: &aiv1alpha1.GitConfig{
							URL:               "https://github.com/example/repo.git",
							Branch:            "feat/test",
							BaseBranch:        "main",
							CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
							CommitAuthor:      "Test Bot",
							CommitEmail:       "bot@test.com",
							AutoPush:          ptr.To(true),
							CreatePR:          ptr.To(true),
							DraftPR:           ptr.To(false),
						},
					},
				},
				OrchestratorAgent: &aiv1alpha1.Agent{
					Spec: aiv1alpha1.AgentSpec{Image: "orchestrator:v1"},
				},
				WorkerEndpoint: "code-worker:8080",
				WorkspacePVC:   "workspace",
				PRD:            `{}`,
			},
			wantErr: false,
			validate: func(t *testing.T, job *batchv1.Job) {
				// Should have git-clone init container
				if len(job.Spec.Template.Spec.InitContainers) != 1 {
					t.Errorf("expected 1 init container, got %d", len(job.Spec.Template.Spec.InitContainers))
					return
				}
				initContainer := job.Spec.Template.Spec.InitContainers[0]
				if initContainer.Name != "git-clone" {
					t.Errorf("expected init container name 'git-clone', got %s", initContainer.Name)
				}
				// Check git-credentials volume mount in init container (security: token read from file)
				foundCredentialsMount := false
				for _, mount := range initContainer.VolumeMounts {
					if mount.Name == "git-credentials" && mount.MountPath == "/secrets/git" && mount.ReadOnly {
						foundCredentialsMount = true
					}
				}
				if !foundCredentialsMount {
					t.Error("git-credentials volume mount not found in init container")
				}
				// Check git-home volume exists
				foundGitHomeVolume := false
				foundCredentialsVolume := false
				for _, vol := range job.Spec.Template.Spec.Volumes {
					if vol.Name == "git-home" {
						foundGitHomeVolume = true
					}
					if vol.Name == "git-credentials" && vol.Secret != nil && vol.Secret.SecretName == "git-creds" {
						foundCredentialsVolume = true
					}
				}
				if !foundGitHomeVolume {
					t.Error("git-home volume not found")
				}
				if !foundCredentialsVolume {
					t.Error("git-credentials volume not found")
				}
				// Check GIT_TOKEN_FILE env var in main container (points to mounted secret)
				foundTokenFile := false
				for _, env := range job.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "GIT_TOKEN_FILE" && env.Value == "/secrets/git/token" {
						foundTokenFile = true
					}
				}
				if !foundTokenFile {
					t.Error("GIT_TOKEN_FILE env var not found in orchestrator container")
				}
				// Check git-credentials volume mount in orchestrator container
				foundOrchestratorCredentialsMount := false
				for _, mount := range job.Spec.Template.Spec.Containers[0].VolumeMounts {
					if mount.Name == "git-credentials" && mount.MountPath == "/secrets/git" && mount.ReadOnly {
						foundOrchestratorCredentialsMount = true
					}
				}
				if !foundOrchestratorCredentialsMount {
					t.Error("git-credentials volume mount not found in orchestrator container")
				}
			},
		},
		{
			name: "orchestrator job with quality gates",
			params: OrchestratorJobParams{
				Task: &aiv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task",
						Namespace: "default",
					},
					Spec: aiv1alpha1.TaskSpec{
						QualityGates: []aiv1alpha1.QualityGate{
							{Name: "lint", Command: []string{"npm", "run", "lint"}},
							{Name: "test", Command: []string{"npm", "test"}},
						},
					},
				},
				OrchestratorAgent: &aiv1alpha1.Agent{
					Spec: aiv1alpha1.AgentSpec{Image: "orchestrator:v1"},
				},
				WorkerEndpoint: "worker:8080",
				WorkspacePVC:   "workspace",
				PRD:            `{}`,
			},
			wantErr: false,
			validate: func(t *testing.T, job *batchv1.Job) {
				for _, env := range job.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "TASK_CONFIG" {
						var config map[string]interface{}
						if err := json.Unmarshal([]byte(env.Value), &config); err != nil {
							t.Errorf("TASK_CONFIG is not valid JSON: %v", err)
							return
						}
						gates, ok := config["qualityGates"]
						if !ok {
							t.Error("qualityGates not found in TASK_CONFIG")
							return
						}
						gatesList := gates.([]interface{})
						if len(gatesList) != 2 {
							t.Errorf("expected 2 quality gates, got %d", len(gatesList))
						}
					}
				}
			},
		},
		{
			name: "orchestrator job with limits",
			params: OrchestratorJobParams{
				Task: &aiv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task",
						Namespace: "default",
					},
					Spec: aiv1alpha1.TaskSpec{
						Limits: &aiv1alpha1.TaskLimits{
							MaxIterations:          ptr.To(int32(50)),
							TotalTimeout:           &metav1.Duration{Duration: 4 * time.Hour},
							MaxConsecutiveFailures: ptr.To(int32(5)),
						},
					},
				},
				OrchestratorAgent: &aiv1alpha1.Agent{
					Spec: aiv1alpha1.AgentSpec{Image: "orchestrator:v1"},
				},
				WorkerEndpoint: "worker:8080",
				WorkspacePVC:   "workspace",
				PRD:            `{}`,
			},
			wantErr: false,
			validate: func(t *testing.T, job *batchv1.Job) {
				// Check job timeout
				expectedTimeout := int64(4 * 60 * 60) // 4 hours in seconds
				if *job.Spec.ActiveDeadlineSeconds != expectedTimeout {
					t.Errorf("expected job timeout %d, got %d", expectedTimeout, *job.Spec.ActiveDeadlineSeconds)
				}
				// Check limits in TASK_CONFIG
				for _, env := range job.Spec.Template.Spec.Containers[0].Env {
					if env.Name == "TASK_CONFIG" {
						var config map[string]interface{}
						if err := json.Unmarshal([]byte(env.Value), &config); err != nil {
							t.Errorf("TASK_CONFIG is not valid JSON: %v", err)
							return
						}
						limits := config["limits"].(map[string]interface{})
						if limits["maxIterations"].(float64) != 50 {
							t.Errorf("expected maxIterations 50, got %v", limits["maxIterations"])
						}
					}
				}
			},
		},
		{
			name: "orchestrator job fails with missing image",
			params: OrchestratorJobParams{
				Task: &aiv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{Name: "test-task", Namespace: "default"},
				},
				OrchestratorAgent: &aiv1alpha1.Agent{
					ObjectMeta: metav1.ObjectMeta{Name: "no-image"},
					Spec:       aiv1alpha1.AgentSpec{Image: ""},
				},
				WorkerEndpoint: "worker:8080",
				WorkspacePVC:   "workspace",
				PRD:            `{}`,
			},
			wantErr:     true,
			errContains: "no image specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := OrchestratorJob(tt.params)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.validate != nil {
				tt.validate(t, job)
			}
		})
	}
}

func TestGitCloneInitContainer(t *testing.T) {
	tests := []struct {
		name     string
		config   *aiv1alpha1.GitConfig
		validate func(t *testing.T, container corev1.Container)
	}{
		{
			name: "basic git clone",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "main",
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				if container.Name != "git-clone" {
					t.Errorf("expected name 'git-clone', got %s", container.Name)
				}
				// Should use default image when not specified
				if container.Image != DefaultGitImage {
					t.Errorf("expected default image %q, got %s", DefaultGitImage, container.Image)
				}
				// Check GIT_URL env
				envMap := make(map[string]string)
				for _, env := range container.Env {
					if env.Value != "" {
						envMap[env.Name] = env.Value
					}
				}
				if envMap["GIT_URL"] != "https://github.com/example/repo.git" {
					t.Errorf("expected GIT_URL, got %s", envMap["GIT_URL"])
				}
				if envMap["GIT_BRANCH"] != "main" {
					t.Errorf("expected GIT_BRANCH=main, got %s", envMap["GIT_BRANCH"])
				}
			},
		},
		{
			name: "git clone with feature branch",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "feat/new-feature",
				BaseBranch:        "develop",
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				envMap := make(map[string]string)
				for _, env := range container.Env {
					if env.Value != "" {
						envMap[env.Name] = env.Value
					}
				}
				if envMap["GIT_BRANCH"] != "feat/new-feature" {
					t.Errorf("expected GIT_BRANCH=feat/new-feature, got %s", envMap["GIT_BRANCH"])
				}
				if envMap["GIT_BASE_BRANCH"] != "develop" {
					t.Errorf("expected GIT_BASE_BRANCH=develop, got %s", envMap["GIT_BASE_BRANCH"])
				}
			},
		},
		{
			name: "git clone with custom depth",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "main",
				Depth:             ptr.To(int32(10)),
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				envMap := make(map[string]string)
				for _, env := range container.Env {
					if env.Value != "" {
						envMap[env.Name] = env.Value
					}
				}
				if envMap["GIT_DEPTH"] != "10" {
					t.Errorf("expected GIT_DEPTH=10, got %s", envMap["GIT_DEPTH"])
				}
			},
		},
		{
			name: "git clone with custom author",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "main",
				CommitAuthor:      "CI Bot",
				CommitEmail:       "ci@example.com",
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				envMap := make(map[string]string)
				for _, env := range container.Env {
					if env.Value != "" {
						envMap[env.Name] = env.Value
					}
				}
				if envMap["GIT_AUTHOR"] != "CI Bot" {
					t.Errorf("expected GIT_AUTHOR='CI Bot', got %s", envMap["GIT_AUTHOR"])
				}
				if envMap["GIT_EMAIL"] != "ci@example.com" {
					t.Errorf("expected GIT_EMAIL='ci@example.com', got %s", envMap["GIT_EMAIL"])
				}
			},
		},
		{
			name: "git clone has correct volume mounts",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				mounts := make(map[string]string)
				readOnly := make(map[string]bool)
				for _, mount := range container.VolumeMounts {
					mounts[mount.Name] = mount.MountPath
					readOnly[mount.Name] = mount.ReadOnly
				}
				if mounts["workspace"] != "/workspace" {
					t.Errorf("expected workspace mount at /workspace, got %s", mounts["workspace"])
				}
				if mounts["git-home"] != "/home/appuser" {
					t.Errorf("expected git-home mount at /home/appuser, got %s", mounts["git-home"])
				}
				// Security: git-credentials should be mounted read-only for secure token access
				if mounts["git-credentials"] != "/secrets/git" {
					t.Errorf("expected git-credentials mount at /secrets/git, got %s", mounts["git-credentials"])
				}
				if !readOnly["git-credentials"] {
					t.Error("git-credentials mount should be read-only")
				}
			},
		},
		{
			name: "git clone with custom image",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "main",
				Image:             "bitnami/git:2.45",
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				if container.Image != "bitnami/git:2.45" {
					t.Errorf("expected custom image 'bitnami/git:2.45', got %s", container.Image)
				}
			},
		},
		{
			name: "git clone with empty image uses default",
			config: &aiv1alpha1.GitConfig{
				URL:               "https://github.com/example/repo.git",
				Branch:            "main",
				Image:             "", // Empty should use default
				CredentialsSecret: corev1.LocalObjectReference{Name: "git-creds"},
			},
			validate: func(t *testing.T, container corev1.Container) {
				if container.Image != DefaultGitImage {
					t.Errorf("expected default image %q when empty, got %s", DefaultGitImage, container.Image)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := gitCloneInitContainer(tt.config)
			tt.validate(t, container)
		})
	}
}

func TestOrchestratorJobLabels(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-task",
		},
	}

	labels := OrchestratorJobLabels(task)

	if labels["fabric.jarsater.ai/task"] != "my-task" {
		t.Errorf("expected task label 'my-task', got %s", labels["fabric.jarsater.ai/task"])
	}
	if labels["app.kubernetes.io/component"] != "task-orchestrator" {
		t.Errorf("expected component label 'task-orchestrator', got %s", labels["app.kubernetes.io/component"])
	}
}

func TestWorkspacePVCName(t *testing.T) {
	task := &aiv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-task",
		},
	}

	name := WorkspacePVCName(task)

	if name != "my-task-workspace" {
		t.Errorf("expected 'my-task-workspace', got %s", name)
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Run("getStringOrDefault", func(t *testing.T) {
		if getStringOrDefault("value", "default") != "value" {
			t.Error("should return value when not empty")
		}
		if getStringOrDefault("", "default") != "default" {
			t.Error("should return default when empty")
		}
	})

	t.Run("getBoolOrDefault", func(t *testing.T) {
		trueVal := true
		falseVal := false
		if getBoolOrDefault(&trueVal, false) != true {
			t.Error("should return true when pointer is true")
		}
		if getBoolOrDefault(&falseVal, true) != false {
			t.Error("should return false when pointer is false")
		}
		if getBoolOrDefault(nil, true) != true {
			t.Error("should return default when nil")
		}
	})

}
