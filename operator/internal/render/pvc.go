package render

import (
	"fmt"

	aiv1alpha1 "github.com/jarsater/mcp-fabric/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultWorkspaceSize is the default PVC size for task workspaces.
	DefaultWorkspaceSize = "1Gi"
)

// TaskWorkspacePVC renders a PersistentVolumeClaim for a Task's workspace.
// The workspace persists across task iterations, allowing incremental work.
func TaskWorkspacePVC(task *aiv1alpha1.Task) *corev1.PersistentVolumeClaim {
	labels := map[string]string{
		"app.kubernetes.io/name":       fmt.Sprintf("%s-workspace", task.Name),
		"app.kubernetes.io/component":  "task-workspace",
		"app.kubernetes.io/managed-by": "mcp-fabric-operator",
		"fabric.jarsater.ai/task":      task.Name,
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WorkspacePVCName(task),
			Namespace: task.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(DefaultWorkspaceSize),
				},
			},
		},
	}
}
