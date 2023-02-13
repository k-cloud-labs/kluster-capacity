/*
Copyright © 2023 k-cloud-labs org

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package clustercompression

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/clustercompression"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

var clusterCompressionLong = dedent.Dedent(`
		cc simulates an API server with initial state copied from the Kubernetes environment
		Its configuration is specified in KUBECONFIG. Simulated API server attempts to scale
		down.The number of nodes specified by the --max-limits flag. If the --max-limits flag
		is not specified, the most likely pods are scheduled onto as few nodes as possible, and a list of nodes that can be taken offline is given.
	`)

func NewClusterCompressionCmd() *cobra.Command {
	opt := options.NewClusterCompressionOptions()

	var cmd = &cobra.Command{
		Use:           "cc",
		Short:         "cc uses simulation scheduling to calculate the number of nodes that can be offline in the cluster",
		Long:          clusterCompressionLong,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := validateOptions(opt)
			if err != nil {
				return err
			}

			err = runClusterCompression(opt)
			if err != nil {
				return err
			}

			return nil
		},
	}

	flags := cmd.Flags()
	flags.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	flags.AddGoFlagSet(flag.CommandLine)
	opt.AddFlags(flags)

	return cmd
}

func validateOptions(opt *options.ClusterCompressionOptions) error {
	_, present := os.LookupEnv("KC_INCLUSTER")
	if !present {
		if len(opt.KubeConfig) == 0 {
			return errors.New("kubeconfig is missing")
		}
	}

	return nil
}

func runClusterCompression(opt *options.ClusterCompressionOptions) error {
	conf := options.NewClusterCompressionConfig(opt)

	cfg, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return err
	}
	conf.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}
	conf.RestConfig = cfg

	reports, err := runCCSimulator(conf)
	if err != nil {
		klog.Errorf("runccsimulator err: %s\n", err.Error())
		return err
	}

	if err := reports.Print(false, ""); err != nil {
		return fmt.Errorf("error while printing: %v\n", err)
	}

	return nil
}

func runCCSimulator(conf *options.ClusterCompressionConfig) (framework.Printer, error) {
	cc, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig)
	if err != nil {
		return nil, err
	}
	s, err := clustercompression.NewCCSimulatorExecutor(cc, conf.RestConfig, conf.Options.MaxLimit, conf.Options.ExcludeNodes, conf.Options.FilterNodeOptions.SkipTaintNode, conf.Options.FilterNodeOptions.SkipNotReadyNode)
	if err != nil {
		return nil, err
	}

	err = s.Initialize()
	if err != nil {
		return nil, err
	}

	err = s.Run()
	if err != nil {
		return nil, err
	}

	return s.Report(), nil
}
