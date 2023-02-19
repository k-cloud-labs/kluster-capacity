package utils

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiv1 "k8s.io/kubernetes/pkg/apis/core/v1"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
)

// IsMirrorPod returns true if the pod is a Mirror Pod.
func IsMirrorPod(pod *corev1.Pod) bool {
	_, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]
	return ok
}

// IsPodTerminating returns true if the pod DeletionTimestamp is set.
func IsPodTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// IsStaticPod returns true if the pod is a static pod.
func IsStaticPod(pod *corev1.Pod) bool {
	source, err := GetPodSource(pod)
	return err == nil && source != "api"
}

// IsCloneSetPod returns true if the pod is a IsCloneSetPod.
func IsCloneSetPod(ownerRefList []metav1.OwnerReference) bool {
	for _, ownerRef := range ownerRefList {
		if ownerRef.Kind == "CloneSet" {
			return true
		}
	}
	return false
}

// IsDaemonsetPod returns true if the pod is a IsDaemonsetPod.
func IsDaemonsetPod(ownerRefList []metav1.OwnerReference) bool {
	for _, ownerRef := range ownerRefList {
		if ownerRef.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// IsPodWithLocalStorage returns true if the pod has local storage.
func IsPodWithLocalStorage(pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath != nil || volume.EmptyDir != nil {
			return true
		}
	}

	return false
}

// GetPodSource returns the source of the pod based on the annotation.
func GetPodSource(pod *corev1.Pod) (string, error) {
	if pod.Annotations != nil {
		if source, ok := pod.Annotations["kubernetes.io/config.source"]; ok {
			return source, nil
		}
	}
	return "", fmt.Errorf("cannot get source of pod %q", pod.UID)
}

func InitPod(podTemplate *corev1.Pod) *corev1.Pod {
	pod := podTemplate.DeepCopy()

	apiv1.SetObjectDefaults_Pod(pod)

	// reset pod
	pod.Spec.NodeName = ""
	pod.Spec.SchedulerName = pkg.SchedulerName
	pod.Namespace = podTemplate.Namespace
	if pod.Namespace == "" {
		pod.Namespace = metav1.NamespaceDefault
	}
	pod.Status = corev1.PodStatus{}

	// use simulated pod name with an index to construct the name
	pod.ObjectMeta.Name = podTemplate.Name
	pod.ObjectMeta.UID = types.UID(uuid.NewV4().String())

	// Add pod provisioner annotation
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}
	pod.ObjectMeta.Annotations[pkg.PodProvisioner] = pkg.SchedulerName

	return pod
}
