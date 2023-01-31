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
package cmds

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lithammer/dedent"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"

	"github.com/k-cloud-labs/kluster-capacity/app/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/capacityestimation"
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
		Use:           "ce --kubeconfig KUBECONFIG --pod-templates PODYAML or ce --kubeconfig KUBECONFIG --pods-from-cluster Namespace/Name",
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
	if len(opt.PodTemplates) == 0 && len(opt.PodsFromCluster) == 0 {
		return errors.New("pod template file and pod from cluster both is missing")
	}

	if len(opt.PodTemplates) != 0 && len(opt.PodsFromCluster) != 0 {
		return errors.New("pod template file and pod from cluster is exclusive")
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

	cfg, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return err
	}

	conf.KubeClient, err = clientset.NewForConfig(cfg)
	if err != nil {
		return err
	}
	conf.RestConfig = cfg

	err = conf.ParseAPISpec()
	if err != nil {
		return fmt.Errorf("failed to parse pod spec file: %v ", err)
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

func runSimulator(conf *options.CapacityEstimationConfig) (framework.Printer, error) {
	run := func(podTemplate *corev1.Pod) (framework.Printer, error) {
		cc, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig)
		if err != nil {
			return nil, err
		}
		s, err := capacityestimation.NewCESimulatorExecutor(cc, conf.RestConfig, podTemplate, conf.Options.MaxLimit, conf.Options.ExcludeNodes)
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

	reports := capacityestimation.CapacityEstimationReviews{}
	g := errgroup.Group{}
	for _, pod := range conf.Pod {
		copy := pod.DeepCopy()
		g.Go(func() error {
			report, err := run(copy)
			if err != nil {
				return err
			}
			reports = append(reports, report.(*capacityestimation.CapacityEstimationReview))
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		return nil, err
	}

	return reports, nil
}
