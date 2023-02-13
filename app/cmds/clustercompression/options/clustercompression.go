package options

import (
	"github.com/spf13/pflag"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

type ClusterCompressionOptions struct {
	SchedulerConfig   string
	KubeConfig        string
	MaxLimit          int
	ExcludeNodes      []string
	FilterNodeOptions FilterNodeOptions
}

type FilterNodeOptions struct {
	SkipTaintNode    bool
	SkipNotReadyNode bool
}

type ClusterCompressionConfig struct {
	KubeClient clientset.Interface
	RestConfig *restclient.Config
	Options    *ClusterCompressionOptions
}

func NewClusterCompressionConfig(opt *ClusterCompressionOptions) *ClusterCompressionConfig {
	return &ClusterCompressionConfig{
		Options: opt,
	}
}

func NewClusterCompressionOptions() *ClusterCompressionOptions {
	return &ClusterCompressionOptions{}
}

func (s *ClusterCompressionOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kubeconfig file to use for the analysis.")
	fs.StringVar(&s.SchedulerConfig, "scheduler-config", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration.")
	fs.IntVar(&s.MaxLimit, "max-limit", 0, "Number of instances of pod to be scheduled after which analysis stops. By default unlimited.")
	fs.BoolVar(&s.FilterNodeOptions.SkipTaintNode, "skipTaintNode", true, "Whether to filter nodes with taint when selecting nodes. By default true.")
	fs.BoolVar(&s.FilterNodeOptions.SkipNotReadyNode, "skipNotReadyNode", true, "Whether to filter nodes with not ready when selecting nodes. By default true.")
	fs.StringSliceVar(&s.ExcludeNodes, "exclude-nodes", s.ExcludeNodes, "Exclude nodes to be scheduled")
}
