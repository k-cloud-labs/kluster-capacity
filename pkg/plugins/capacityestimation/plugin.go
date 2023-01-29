package capacityestimation

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
)

const Name = "CapacityEstimationBinder"

type CapacityEstimationBinder struct {
	postBindHook func(*corev1.Pod) error
}

func New(postBindHook func(*corev1.Pod) error) (framework.Plugin, error) {
	return &CapacityEstimationBinder{
		postBindHook: postBindHook,
	}, nil
}

func (b *CapacityEstimationBinder) Name() string {
	return Name
}

func (b *CapacityEstimationBinder) PostBind(_ context.Context, _ *framework.CycleState, pod *corev1.Pod, _ string) {
	// ignore non simulated pod
	if metav1.HasAnnotation(pod.ObjectMeta, pkgframework.PodProvisioner) {
		if err := b.postBindHook(pod); err != nil {
			framework.NewStatus(framework.Error, fmt.Sprintf("Invoking postBindHook gives an error: %v", err))
		}
	}
}
