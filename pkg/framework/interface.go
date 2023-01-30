package framework

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	Namespace      = "kclabs-system"
	PodProvisioner = "kc.k-cloud-labs.io/provisioned-by"
	SchedulerName  = "simulator-scheduler"
)

type Simulator interface {
	Run() error
	CreatePod(pod *corev1.Pod) error
	UpdateStatus(pod *corev1.Pod)
	Status() Status
	Stop(reason string)
}

type SimulatorExecutor interface {
	Run() error
	Initialize() error
	Report() Printer
}

type Printer interface {
	Print(verbose bool, format string) error
}
