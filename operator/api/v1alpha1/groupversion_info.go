// Package v1alpha1 contains API Schema definitions for the fabric.jarsater.ai v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=fabric.jarsater.ai
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "fabric.jarsater.ai", Version: "v1alpha1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	// scheme.Builder is the standard kubebuilder scaffold. Its deprecation
	// suggests the apimachinery runtime.NewSchemeBuilder pattern, which is not
	// a drop-in replacement for the generated per-type Register calls, so we
	// keep the scaffold and silence the single deprecation here.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion} //nolint:staticcheck // see comment above

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
