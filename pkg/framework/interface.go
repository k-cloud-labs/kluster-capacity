package framework

import (
	clientset "k8s.io/client-go/kubernetes"
)

const (
	Namespace      = "kclabs-system"
	PodProvisioner = "kc.k-cloud-labs.io/provisioned-by"
	SchedulerName  = "simulator-scheduler"
)

type Simulator interface {
	Run() error
	SyncWithClient(p clientset.Interface) error
	//SyncWithInformerFactory(factory informers.SharedInformerFactory) error
	Report() Printer
}

type Printer interface {
	Print(verbose bool, format string) error
}
