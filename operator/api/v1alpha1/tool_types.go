package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ToolDefinition defines a single tool within a Tool resource.
type ToolDefinition struct {
	// Name is the tool function name (matches @tool decorated function).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Description explains what the tool does.
	// +optional
	Description string `json:"description,omitempty"`

	// InputSchema is the JSON Schema for tool input parameters.
	// +optional
	InputSchema *JSONSchemaProps `json:"inputSchema,omitempty"`

	// OutputSchema is the JSON Schema for tool output.
	// +optional
	OutputSchema *JSONSchemaProps `json:"outputSchema,omitempty"`
}

// JSONSchemaProps is a simplified JSON Schema for tool parameters.
// Uses apiextensionsv1.JSON for flexible nested schema support.
type JSONSchemaProps struct {
	// Type is the JSON Schema type (object, string, number, etc.).
	// +kubebuilder:validation:Enum=object;string;number;integer;boolean;array
	Type string `json:"type,omitempty"`

	// Properties defines object properties as raw JSON (when type=object).
	// +optional
	Properties *apiextensionsv1.JSON `json:"properties,omitempty"`

	// Required lists required property names.
	// +optional
	Required []string `json:"required,omitempty"`

	// Items defines array item schema as raw JSON (when type=array).
	// +optional
	Items *apiextensionsv1.JSON `json:"items,omitempty"`

	// Description of this schema element.
	// +optional
	Description string `json:"description,omitempty"`
}

// ToolSpec defines the desired state of Tool.
type ToolSpec struct {
	// Image is the OCI image containing the tool package code.
	// The image should contain Python code with @tool decorated functions.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// ImagePullPolicy determines when to pull the image.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +kubebuilder:default=IfNotPresent
	// +optional
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// ImagePullSecrets are references to secrets for pulling the image.
	// +optional
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Tools declares the available tools in this package.
	// If empty, tools will be discovered at runtime or via introspection.
	// +optional
	Tools []ToolDefinition `json:"tools,omitempty"`

	// EntryModule is the Python module path to import (e.g., "mypackage.tools").
	// +optional
	EntryModule string `json:"entryModule,omitempty"`
}

// LocalObjectReference references a local object by name.
type LocalObjectReference struct {
	// Name of the referenced object.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

// ToolStatus defines the observed state of Tool.
type ToolStatus struct {
	// Ready indicates the Tool is validated and available.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// ObservedGeneration is the last observed generation.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// AvailableTools lists tools discovered or declared in the package.
	// +optional
	AvailableTools []ToolDefinition `json:"availableTools,omitempty"`

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
// +kubebuilder:resource:shortName=tl
// +kubebuilder:printcolumn:name="Ready",type="boolean",JSONPath=".status.ready",description="Tool ready"
// +kubebuilder:printcolumn:name="Tools",type="integer",JSONPath=".status.availableTools",description="Number of tools"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Tool declares a Strands tool bundle containing @tool decorated functions.
type Tool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ToolSpec   `json:"spec,omitempty"`
	Status ToolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ToolList contains a list of Tool.
type ToolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tool{}, &ToolList{})
}
