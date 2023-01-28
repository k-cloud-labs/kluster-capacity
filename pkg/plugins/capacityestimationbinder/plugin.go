package capacityestimationbinder

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
)

const Name = "CapacityEstimationBinder"

type CapacityEstimationBinder struct {
	client       kubernetes.Interface
	postBindHook func(*corev1.Pod) error
}

func New(client kubernetes.Interface, _ runtime.Object, _ framework.Handle, postBindHook func(*corev1.Pod) error) (framework.Plugin, error) {
	return &CapacityEstimationBinder{
		client:       client,
		postBindHook: postBindHook,
	}, nil
}

func (b *CapacityEstimationBinder) Name() string {
	return Name
}

func (b *CapacityEstimationBinder) Bind(ctx context.Context, state *framework.CycleState, p *corev1.Pod, nodeName string) *framework.Status {
	pod, err := b.client.CoreV1().Pods(p.Namespace).Get(context.TODO(), p.Name, metav1.GetOptions{})
	if err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to bind: %v", err))
	}
	updatedPod := pod.DeepCopy()
	updatedPod.Spec.NodeName = nodeName
	updatedPod.Status.Phase = corev1.PodRunning

	if _, err = b.client.CoreV1().Pods(pod.Namespace).Update(ctx, updatedPod, metav1.UpdateOptions{}); err != nil {
		return framework.NewStatus(framework.Error, fmt.Sprintf("Unable to update binded pod: %v", err))
	}

	// ignore non simulated pod
	if metav1.HasAnnotation(pod.ObjectMeta, pkgframework.PodProvisioner) {
		if err := b.postBindHook(updatedPod); err != nil {
			framework.NewStatus(framework.Error, fmt.Sprintf("Invoking postBindHook gives an error: %v", err))
		}
	}

	return nil
}
