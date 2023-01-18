/*
Copyright © 2023 likakuli <1154584512@qq.com>

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
package cmds

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"

	"github.com/k-cloud-labs/kluster-capacity/app/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/ce"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

var capacityEstimationLong = dedent.Dedent(`
		ce simulates an API server with initial state copied from the Kubernetes environment
		with its configuration specified in KUBECONFIG. The simulated API server tries to schedule the number of
		pods specified by --max-limits flag. If the --max-limits flag is not specified, pods are scheduled until
		the simulated API server runs out of resources.
	`)

func NewCapacityEstimationCmd() *cobra.Command {
	opt := options.NewCapacityEstimationOptions()

	var cmd = &cobra.Command{
		Use:           "ce --kubeconfig KUBECONFIG --pod-template PODYAML",
		Short:         "ce is used for simulating scheduling of one or multiple pods",
		Long:          capacityEstimationLong,
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

func validate(opt *options.CapacityEstimationOptions) error {
	if len(opt.PodTemplate) == 0 {
		return errors.New("pod template file is missing")
	}

	_, present := os.LookupEnv("KC_INCLUSTER")
	if !present {
		if len(opt.KubeConfig) == 0 {
			return errors.New("kubeconfig is missing")
		}
	}

	return nil
}

func run(opt *options.CapacityEstimationOptions) error {
	conf := options.NewCapacityEstimationConfig(opt)

	cc, err := utils.BuildKubeSchedulerCompletedConfig(opt.SchedulerConfig)
	if err != nil {
		return err
	}

	cfg, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return err
	}

	err = conf.ParseAPISpec()
	if err != nil {
		return fmt.Errorf("failed to parse pod spec file: %v ", err)
	}

	conf.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}
	conf.RestConfig = cfg

	report, err := runSimulator(conf, cc)
	if err != nil {
		return err
	}

	if err := report.Print(conf.Options.Verbose, conf.Options.OutputFormat); err != nil {
		return fmt.Errorf("error while printing: %v", err)
	}

	return nil
}

func runSimulator(conf *options.CapacityEstimationConfig, kubeSchedulerConfig *schedconfig.CompletedConfig) (framework.Printer, error) {
	s, err := ce.NewCESimulator(kubeSchedulerConfig, conf.RestConfig, conf.Pod, conf.Options.MaxLimit, conf.Options.ExcludeNodes)
	if err != nil {
		return nil, err
	}

	err = s.SyncWithClient(conf.KubeClient)
	if err != nil {
		return nil, err
	}

	err = s.Run()
	if err != nil {
		return nil, err
	}

	return s.Report(), nil
}
