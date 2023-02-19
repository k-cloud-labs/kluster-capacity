package pkg

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	Pods               []*corev1.Pod
	ScaleDownNodeNames []string
	Nodes              map[string]corev1.Node
	StopReason         string
}

type Simulator interface {
	Run() error
	InitTheWorld(objs ...runtime.Object) error
	CreatePod(pod *corev1.Pod) error
	UpdateStatus(pod ...*corev1.Pod)
	UpdateStatusScaleDownNodeNames(nodeName string)
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
