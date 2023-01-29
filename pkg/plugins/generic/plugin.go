package generic

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const Name = "GenericBinder"

type GenericBinder struct {
	client kubernetes.Interface
}

func New(client kubernetes.Interface) (framework.Plugin, error) {
	return &GenericBinder{
		client: client,
	}, nil
}

func (b *GenericBinder) Name() string {
	return Name
}

func (b *GenericBinder) Bind(ctx context.Context, state *framework.CycleState, p *corev1.Pod, nodeName string) *framework.Status {
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

	return nil
}

func (b *GenericBinder) PreBind(ctx context.Context, state *framework.CycleState, p *corev1.Pod, nodeName string) *framework.Status {
	return nil
}
