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
package schedulersimulation

import (
	"errors"
	"flag"
	"fmt"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/schedulersimulation/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/schedulersimulation"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

var schedulerSimulationLong = dedent.Dedent(`
		ss simulates an API server with initial state copied from the Kubernetes environment
		with its configuration specified in KUBECONFIG. The simulated API server tries to schedule the number of
		pods from existing cluster.
	`)

func NewSchedulerSimulationCmd() *cobra.Command {
	opt := options.NewSchedulerSimulationOptions()

	// ssCmd represents the ss command
	var cmd = &cobra.Command{
		Use:           "ss",
		Short:         "ss is used for simulating scheduling of pods",
		Long:          schedulerSimulationLong,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := validate(opt)
			if err != nil {
				return err
			}

			err = run(opt)
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

func validate(opt *options.SchedulerSimulationOptions) error {
	if opt.SourceFrom == options.FromCluster && len(opt.KubeConfig) == 0 {
		return errors.New("kubeconfig must be specified when source-from is cluster")
	}

	if opt.SourceFrom == options.FromSnapshot && len(opt.Snapshot) == 0 {
		return errors.New("snapshot must be specified when source-from is snapshot")
	}

	return nil
}

func run(opt *options.SchedulerSimulationOptions) error {
	conf := options.NewSchedulerSimulationConfig(opt)

	if opt.SourceFrom == options.FromCluster {
		cfg, err := utils.BuildRestConfig(conf.Options.KubeConfig)
		if err != nil {
			return err
		}

		conf.KubeClient, err = clientset.NewForConfig(cfg)
		if err != nil {
			return err
		}
		conf.RestConfig = cfg
	}

	reports, err := runSimulator(conf)
	if err != nil {
		return err
	}

	if err := reports.Print(conf.Options.Verbose, conf.Options.OutputFormat); err != nil {
		return fmt.Errorf("error while printing: %v", err)
	}

	return nil
}

func runSimulator(conf *options.SchedulerSimulationConfig) (framework.Printer, error) {
	cc, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig)
	if err != nil {
		return nil, err
	}
	s, err := schedulersimulation.NewSSSimulatorExecutor(cc, conf.RestConfig, conf.Options.ExitCondition, conf.Options.ExcludeNodes)
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
