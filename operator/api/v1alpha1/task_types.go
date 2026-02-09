package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TaskPhase represents the current phase of the Task execution.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Paused
type TaskPhase string

const (
	// TaskPhasePending indicates the task has not started yet.
	TaskPhasePending TaskPhase = "Pending"

	// TaskPhaseRunning indicates the task is actively executing iterations.
	TaskPhaseRunning TaskPhase = "Running"

	// TaskPhaseCompleted indicates all tasks have passed.
	TaskPhaseCompleted TaskPhase = "Completed"

	// TaskPhaseFailed indicates the task failed after exhausting retries or hitting limits.
	TaskPhaseFailed TaskPhase = "Failed"

	// TaskPhasePaused indicates the task is paused (e.g., waiting for human review).
	TaskPhasePaused TaskPhase = "Paused"
)

// AgentReference refers to an Agent resource.
type AgentReference struct {
	// Name of the Agent resource.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the Agent (defaults to Task namespace).
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// TaskSourceType specifies the type of task source.
// +kubebuilder:validation:Enum=configmap;secret;inline
type TaskSourceType string

const (
	TaskSourceTypeConfigMap TaskSourceType = "configmap"
	TaskSourceTypeSecret    TaskSourceType = "secret"
	TaskSourceTypeInline    TaskSourceType = "inline"
)

// TaskSource defines where to read the PRD/task list from.
type TaskSource struct {
	// Type of the task source.
	// +kubebuilder:validation:Required
	// +kubebuilder:default=configmap
	Type TaskSourceType `json:"type"`

	// ConfigMapRef references a ConfigMap containing the PRD.
	// +optional
	ConfigMapRef *corev1.ConfigMapKeySelector `json:"configMapRef,omitempty"`

	// SecretRef references a Secret containing the PRD.
	// +optional
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`

	// Inline contains the PRD directly in the spec.
	// +optional
	Inline string `json:"inline,omitempty"`
}

// TaskLimits defines execution constraints.
type TaskLimits struct {
	// MaxIterations is the maximum number of loop iterations.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=100
	// +optional
	MaxIterations *int32 `json:"maxIterations,omitempty"`

	// IterationTimeout is the maximum duration for a single iteration.
	// +kubebuilder:default="30m"
	// +optional
	IterationTimeout *metav1.Duration `json:"iterationTimeout,omitempty"`

	// TotalTimeout is the maximum total duration for the entire task.
	// +kubebuilder:default="24h"
	// +optional
	TotalTimeout *metav1.Duration `json:"totalTimeout,omitempty"`

	// MaxConsecutiveFailures is the number of consecutive failures before pausing/failing.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	// +optional
	MaxConsecutiveFailures *int32 `json:"maxConsecutiveFailures,omitempty"`

	// MaxJobRecreations is the maximum number of times a lost Job will be recreated
	// before the task is marked as failed.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxJobRecreations *int32 `json:"maxJobRecreations,omitempty"`
}

// GitProvider specifies the Git hosting provider.
// +kubebuilder:validation:Enum=github;gitlab;bitbucket
type GitProvider string

const (
	GitProviderGitHub    GitProvider = "github"
	GitProviderGitLab    GitProvider = "gitlab"
	GitProviderBitbucket GitProvider = "bitbucket"
)

// GitConfig defines Git repository settings for task artifacts.
// Only cloning existing repositories is supported - creating new repos is not allowed.
type GitConfig struct {
	// URL is the repository URL to clone.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// Provider is the Git hosting provider (github, gitlab, bitbucket).
	// +kubebuilder:default=github
	// +optional
	Provider GitProvider `json:"provider,omitempty"`

	// Image is the container image to use for git operations.
	// +kubebuilder:default="alpine/git:2.43"
	// +optional
	Image string `json:"image,omitempty"`

	// Branch is the branch to work on.
	// +kubebuilder:default="main"
	// +optional
	Branch string `json:"branch,omitempty"`

	// BaseBranch is the branch to create the working branch from (for feature branches).
	// If set, creates a new branch from BaseBranch before starting work.
	// +optional
	BaseBranch string `json:"baseBranch,omitempty"`

	// Depth for shallow clone (0 = full clone).
	// +kubebuilder:default=1
	// +optional
	Depth *int32 `json:"depth,omitempty"`

	// CredentialsSecret references a Secret containing git credentials.
	// Required key: "token" (GitHub PAT or equivalent).
	// +kubebuilder:validation:Required
	CredentialsSecret corev1.LocalObjectReference `json:"credentialsSecret"`

	// CommitAuthor is the author name for commits.
	// +kubebuilder:default="MCP Fabric Task"
	// +optional
	CommitAuthor string `json:"commitAuthor,omitempty"`

	// CommitEmail is the author email for commits.
	// +kubebuilder:default="task@mcp-fabric.local"
	// +optional
	CommitEmail string `json:"commitEmail,omitempty"`

	// AutoPush enables automatic push on completion.
	// +kubebuilder:default=true
	// +optional
	AutoPush *bool `json:"autoPush,omitempty"`

	// CreatePR enables automatic PR creation on completion.
	// +kubebuilder:default=true
	// +optional
	CreatePR *bool `json:"createPR,omitempty"`

	// DraftPR creates PR as draft.
	// +kubebuilder:default=true
	// +optional
	DraftPR *bool `json:"draftPR,omitempty"`

	// PRTitle is the title for the PR (default: "Task: {task-name}").
	// +optional
	PRTitle string `json:"prTitle,omitempty"`

	// PRBody is the body template for the PR.
	// Supports placeholders: {task}, {completed}, {total}.
	// +optional
	PRBody string `json:"prBody,omitempty"`
}

// QualityGate defines a command to run as a quality check.
type QualityGate struct {
	// Name identifies this quality gate.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Command is the command to execute.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Command []string `json:"command"`

	// FailurePolicy determines what happens if the gate fails.
	// +kubebuilder:validation:Enum=Fail;Retry;Ignore
	// +kubebuilder:default=Fail
	// +optional
	FailurePolicy string `json:"failurePolicy,omitempty"`

	// Timeout for the quality gate command.
	// +kubebuilder:default="5m"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// TaskSpec defines the desired state of Task.
type TaskSpec struct {
	// WorkerRef references the agent that executes individual tasks.
	// +kubebuilder:validation:Required
	WorkerRef AgentReference `json:"workerRef"`

	// OrchestratorRef references the orchestrator agent that manages task execution.
	// If not specified, defaults to "task-orchestrator" in the same namespace.
	// +optional
	OrchestratorRef *AgentReference `json:"orchestratorRef,omitempty"`

	// TaskSource defines where to read the PRD/task list from.
	// +kubebuilder:validation:Required
	TaskSource TaskSource `json:"taskSource"`

	// Limits defines execution constraints.
	// +optional
	Limits *TaskLimits `json:"limits,omitempty"`

	// QualityGates defines commands to run as quality checks after each task.
	// +optional
	QualityGates []QualityGate `json:"qualityGates,omitempty"`

	// Git defines Git repository settings for the task workspace.
	// When configured, the repo is cloned before execution and changes are pushed on completion.
	// +optional
	Git *GitConfig `json:"git,omitempty"`

	// Paused indicates the task should not run iterations (for manual review).
	// +kubebuilder:default=false
	// +optional
	Paused bool `json:"paused,omitempty"`

	// Context provides additional context to pass to the orchestrator.
	// +optional
	Context string `json:"context,omitempty"`
}

// IterationResult captures the outcome of a single iteration.
type IterationResult struct {
	// Iteration number (1-based).
	Iteration int32 `json:"iteration"`

	// TaskID is the ID of the task that was attempted.
	// +optional
	TaskID string `json:"taskId,omitempty"`

	// TaskTitle is the title of the task that was attempted.
	// +optional
	TaskTitle string `json:"taskTitle,omitempty"`

	// Passed indicates if the task passed quality gates.
	Passed bool `json:"passed"`

	// StartedAt is when this iteration started.
	StartedAt metav1.Time `json:"startedAt"`

	// CompletedAt is when this iteration completed.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// Error message if the iteration failed.
	// +optional
	Error string `json:"error,omitempty"`

	// Learnings captured during this iteration.
	// +optional
	Learnings string `json:"learnings,omitempty"`
}

// TaskStatus defines the observed state of Task.
type TaskStatus struct {
	// Phase is the current execution phase.
	// +optional
	Phase TaskPhase `json:"phase,omitempty"`

	// CurrentIteration is the current/last iteration number.
	// +optional
	CurrentIteration int32 `json:"currentIteration,omitempty"`

	// CompletedTasks is the number of tasks marked as passed.
	// +optional
	CompletedTasks int32 `json:"completedTasks,omitempty"`

	// TotalTasks is the total number of tasks in the PRD.
	// +optional
	TotalTasks int32 `json:"totalTasks,omitempty"`

	// LastTaskID is the ID of the last attempted task.
	// +optional
	LastTaskID string `json:"lastTaskId,omitempty"`

	// ConsecutiveFailures is the current count of consecutive failures.
	// +optional
	ConsecutiveFailures int32 `json:"consecutiveFailures,omitempty"`

	// StartedAt is when the task execution started.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// LastIterationAt is when the last iteration ran.
	// +optional
	LastIterationAt *metav1.Time `json:"lastIterationAt,omitempty"`

	// CompletedAt is when the task completed (successfully or with failure).
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`

	// RecentIterations contains the most recent iteration results.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	RecentIterations []IterationResult `json:"recentIterations,omitempty"`

	// RepositoryURL is the URL of the Git repository being used.
	// +optional
	RepositoryURL string `json:"repositoryUrl,omitempty"`

	// LastCommitSHA is the SHA of the most recent commit.
	// +optional
	LastCommitSHA string `json:"lastCommitSha,omitempty"`

	// PullRequestURL is the URL of the PR created when task completed.
	// +optional
	PullRequestURL string `json:"pullRequestUrl,omitempty"`

	// ObservedGeneration is the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Message provides additional status information.
	// +optional
	Message string `json:"message,omitempty"`

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
// +kubebuilder:resource:shortName=tk
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Task phase"
// +kubebuilder:printcolumn:name="Iteration",type="integer",JSONPath=".status.currentIteration",description="Current iteration"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.completedTasks",description="Completed tasks"
// +kubebuilder:printcolumn:name="Total",type="string",JSONPath=".status.totalTasks",description="Total tasks"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Task defines an autonomous task execution loop following the Ralph pattern.
// It orchestrates repeated AI agent iterations until all PRD items are complete.
type Task struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TaskSpec   `json:"spec,omitempty"`
	Status TaskStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TaskList contains a list of Task.
type TaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Task `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Task{}, &TaskList{})
}
