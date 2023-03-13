package cmds

import (
	"os"
	"path/filepath"
)

type Options struct {
	SchedulerConfig string
	KubeConfig      string
	Verbose         bool
	OutputFormat    string
	// file to load initial data instead of from k8s cluster
	Snapshot string
	// file to save the result
	SaveTo       string
	ExcludeNodes []string
	MaxLimit     int
}

func (o *Options) Default() {
	if len(o.KubeConfig) == 0 {
		config := os.Getenv("KUBECONFIG")
		if len(config) == 0 {
			config = filepath.Join(os.Getenv("HOME"), ".kube/config")
		}
		o.KubeConfig = config
	}
}
