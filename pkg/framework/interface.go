package framework

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	Namespace      = "kclabs-system"
	PodProvisioner = "kc.k-cloud-labs.io/provisioned-by"
	SchedulerName  = "simulator-scheduler"
)

type Simulator interface {
	Run() error
	InitializeWithClient(client clientset.Interface) error
	InitializeWithInformerFactory(factory informers.SharedInformerFactory) error
	CreatePod(pod *corev1.Pod) error
	UpdateStatus(pod *corev1.Pod)
	Status() Status
	Stop(reason string)
}

type SimulatorExecutor interface {
	Run() error
	InitializeWithClient(client clientset.Interface) error
	InitializeWithInformerFactory(factory informers.SharedInformerFactory) error
	Report() Printer
}

type Printer interface {
	Print(verbose bool, format string) error
}
