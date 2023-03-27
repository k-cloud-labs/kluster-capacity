package options

import (
	"github.com/spf13/pflag"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds"
)

type ClusterCompressionOptions struct {
	cmds.Options
	FilterNodeOptions FilterNodeOptions
}

type FilterNodeOptions struct {
	ExcludeNotReadyNode bool
	ExcludeTaintNode    bool
	IgnoreStaticPod     bool
	IgnoreMirrorPod     bool
	IgnoreCloneSet      bool
	IgnoreVolumePod     bool
}

type ClusterCompressionConfig struct {
	Options *ClusterCompressionOptions
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
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|default (Note: output is not versioned or guaranteed to be stable across releases)")
	fs.StringVar(&s.SchedulerConfig, "schedulerconfig", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration.")
	fs.IntVar(&s.MaxLimit, "max-limit", 0, "Number of instances of node to be scale down after which analysis stops.. By default unlimited.")
	fs.BoolVar(&s.FilterNodeOptions.ExcludeTaintNode, "exclude-taint-node", true, "Whether to filter nodes with taint when selecting nodes. By default true.")
	fs.BoolVar(&s.FilterNodeOptions.ExcludeNotReadyNode, "exclude-not-ready-node", true, "Whether to filter nodes with not ready when selecting nodes. By default true.")
	fs.BoolVar(&s.FilterNodeOptions.IgnoreStaticPod, "ignore-static-pod", false, "Whether to ignore nodes with static pods when filtering nodes. By default true.")
	fs.BoolVar(&s.FilterNodeOptions.IgnoreMirrorPod, "ignore-mirror-pod", false, "Whether to ignore nodes with mirror pods when filtering nodes. By default false.")
	fs.BoolVar(&s.FilterNodeOptions.IgnoreCloneSet, "ignore-cloneset", false, "Whether to ignore nodes with cloneSet pods when filtering nodes. By default false.")
	fs.BoolVar(&s.FilterNodeOptions.IgnoreVolumePod, "ignore-volume-pod", false, "Whether to ignore nodes with volume pods when filtering nodes. By default false.")
	fs.StringSliceVar(&s.ExcludeNodes, "exclude-nodes", s.ExcludeNodes, "Exclude nodes to be scheduled")
	fs.BoolVar(&s.Verbose, "verbose", s.Verbose, "Verbose mode")
}
