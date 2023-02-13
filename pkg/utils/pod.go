package utils

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
