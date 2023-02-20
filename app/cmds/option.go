package cmds

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
