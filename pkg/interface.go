package pkg

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Framework need to be implemented by all scheduler framework
type Framework interface {
	Run(init func() error) error
	Initialize(objs ...runtime.Object) error
	CreatePod(pod *corev1.Pod) error
	UpdateEstimationPods(pod ...*corev1.Pod)
	UpdateNodesToScaleDown(nodeName string)
	Status() *Status
	GetPodsByNode(nodeName string) ([]*corev1.Pod, error)
	Stop(reason string) error
}

// Simulator need to be implemented by all simulator
type Simulator interface {
	Run() error
	Initialize(objs ...runtime.Object) error
	Report() Printer
}

type Printer interface {
	Print(verbose bool, format string) error
}
