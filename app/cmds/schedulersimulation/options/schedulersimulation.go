package options

import (
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// FromCluster represent an existing cluster without pods
	FromCluster = "Cluster"
	// FromSnapshot represent a snapshot
	FromSnapshot = "Snapshot"

	// ExitWhenAllScheduled means exit when all pods have been scheduled once
	ExitWhenAllScheduled = "AllScheduled"
	// ExitWhenAllSucceed means exit when all pods have been scheduled successfully
	ExitWhenAllSucceed = "AllSucceed"
)

type Snapshot struct {
	// key is gk
	Objects map[string][]runtime.Object `json:"objects"`
}

type SchedulerSimulationOptions struct {
	SchedulerConfig string
	KubeConfig      string
	Snapshot        string
	OutputFormat    string
	Verbose         bool
	SaveTo          string
	// Cluster, Snapshot
	SourceFrom               string
	ExitCondition            string
	ExcludeNodes             []string
	IgnorePodsOnExcludeNodes bool
}

type SchedulerSimulationConfig struct {
	Options  *SchedulerSimulationOptions
	InitObjs []runtime.Object
}

func NewSchedulerSimulationOptions() *SchedulerSimulationOptions {
	return &SchedulerSimulationOptions{}
}

func NewSchedulerSimulationConfig(option *SchedulerSimulationOptions) *SchedulerSimulationConfig {
	return &SchedulerSimulationConfig{
		Options: option,
	}
}

func (s *SchedulerSimulationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kubeconfig file to use for the analysis")
	fs.StringVar(&s.SchedulerConfig, "scheduler-config", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration. Used when source-from is cluster")
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|yaml")
	fs.StringSliceVar(&s.ExcludeNodes, "exclude-nodes", s.ExcludeNodes, "Exclude nodes to be scheduled")
	fs.BoolVarP(&s.IgnorePodsOnExcludeNodes, "ignore-pods-on-excludes-nodes", "i", true, "Whether ignore the pods on the excludes nodes. By default true")
	fs.StringVar(&s.Snapshot, "snapshot", s.Snapshot, "Path of snapshot to initialize the world. Used when source-from is snapshot")
	fs.StringVar(&s.SourceFrom, "source-from", "Cluster", "Source of the init data. One of: Cluster|Snapshot")
	fs.StringVar(&s.ExitCondition, "exit-condition", "AllSucceed", "Exit condition of the simulator. One of: AllScheduled|AllSucceed")
	fs.BoolVar(&s.Verbose, "verbose", s.Verbose, "Verbose mode")
	fs.StringVarP(&s.SaveTo, "save", "s", s.SaveTo, "File path to save the simulation result")
}
