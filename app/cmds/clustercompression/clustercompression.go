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
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/clustercompression"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

var clusterCompressionLong = dedent.Dedent(`
	The "cc" tool simulates an API server by copying the initial state from the Kubernetes environment, 
	using the configuration specified in KUBECONFIG. It attempts to scale down the number of nodes to 
	the limit specified by the --max-limits flag, and if this flag is not provided, it schedules pods 
	onto as few nodes as possible and provides a list of nodes that can be taken offline.
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

	klog.InitFlags(nil)
	flag.Parse()

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
	defer klog.Flush()
	conf := options.NewClusterCompressionConfig(opt)

	cfg, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return err
	}
	conf.RestConfig = cfg

	reports, err := runCCSimulator(conf)
	if err != nil {
		klog.Errorf("runCCSimulator err: %s\n", err.Error())
		return err
	}

	if err := reports.Print(false, conf.Options.OutputFormat); err != nil {
		return fmt.Errorf("error while printing: %v\n", err)
	}
	return nil
}

func runCCSimulator(conf *options.ClusterCompressionConfig) (framework.Printer, error) {
	s, err := clustercompression.NewCCSimulatorExecutor(conf)
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
