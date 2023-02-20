package pkg

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	Pods             []*corev1.Pod          `json:"pods"`
	Nodes            map[string]corev1.Node `json:"nodes"`
	NodesToScaleDown []string               `json:"nodes_to_scale_down"`
	StopReason       string                 `json:"stop_reason"`
}

type Simulator interface {
	Run() error
	InitTheWorld(objs ...runtime.Object) error
	CreatePod(pod *corev1.Pod) error
	UpdateScheduledPods(pod ...*corev1.Pod)
	UpdateNodesToScaleDown(nodeName string)
	Status() Status
	GetPodsByNode(nodeName string) ([]*corev1.Pod, error)
	Stop(reason string) error
}

type SimulatorExecutor interface {
	Run() error
	Initialize(objs ...runtime.Object) error
	Report() Printer
}

type Printer interface {
	Print(verbose bool, format string) error
}
