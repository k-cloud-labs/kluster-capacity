package ce

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/plugins/capacityestimationbinder"
)

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkgframework.Simulator

	podGenerator PodGenerator
	simulatedPod *corev1.Pod
	maxSimulated int
	simulated    int
}

// NewCESimulator create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCESimulatorExecutor(kubeSchedulerConfig *schedconfig.CompletedConfig, kubeConfig *restclient.Config, simulatedPod *corev1.Pod, maxPods int, excludeNodes []string) (pkgframework.SimulatorExecutor, error) {
	s := &simulator{
		podGenerator: NewSinglePodGenerator(simulatedPod),
		simulatedPod: simulatedPod,
		simulated:    0,
		maxSimulated: maxPods,
	}

	genericSimulator, err := pkgframework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
		pkgframework.WithExcludeNodes(excludeNodes),
		pkgframework.WithOutOfTreeRegistry(frameworkruntime.Registry{
			capacityestimationbinder.Name: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
				return capacityestimationbinder.New(kubeSchedulerConfig.Client, configuration, f, s.postBindHook)
			},
		}),
		pkgframework.WithCustomBind(kubeschedulerconfig.PluginSet{
			Enabled:  []kubeschedulerconfig.Plugin{{Name: capacityestimationbinder.Name}},
			Disabled: []kubeschedulerconfig.Plugin{{Name: defaultbinder.Name}},
		}),
		pkgframework.WithCustomEventHandlers([]func(){
			func() {
				_, _ = kubeSchedulerConfig.InformerFactory.Core().V1().Pods().Informer().AddEventHandler(
					cache.FilteringResourceEventHandler{
						FilterFunc: func(obj interface{}) bool {
							if pod, ok := obj.(*corev1.Pod); ok && pod.Spec.SchedulerName == pkgframework.SchedulerName &&
								metav1.HasAnnotation(pod.ObjectMeta, pkgframework.PodProvisioner) {
								return true
							}
							return false
						},
						Handler: cache.ResourceEventHandlerFuncs{
							UpdateFunc: func(oldObj, newObj interface{}) {
								if pod, ok := newObj.(*corev1.Pod); ok {
									for _, podCondition := range pod.Status.Conditions {
										// Only for pending pods provisioned by ce
										if podCondition.Type == corev1.PodScheduled && podCondition.Status == corev1.ConditionFalse &&
											podCondition.Reason == corev1.PodReasonUnschedulable {
											s.Stop(fmt.Sprintf("%v: %v", podCondition.Reason, podCondition.Message))
										}
									}
								}
							},
						},
					},
				)
			},
		}))
	if err != nil {
		return nil, err
	}

	s.Simulator = genericSimulator

	return s, nil
}

func (s *simulator) InitializeWithClient(client clientset.Interface) error {
	err := s.Simulator.InitializeWithClient(client)
	if err != nil {
		return err
	}

	// create first pod
	return s.createNextPod()
}

func (s *simulator) Report() pkgframework.Printer {
	return generateReport([]*corev1.Pod{s.simulatedPod}, s.Status())
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {
	s.Simulator.UpdateStatus(bindPod)

	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		s.Simulator.Stop(fmt.Sprintf("LimitReached: Maximum number of pods simulated: %v", s.maxSimulated))
		return nil
	}

	if err := s.createNextPod(); err != nil {
		return fmt.Errorf("unable to create next pod for simulated scheduling: %v", err)
	}
	return nil
}

func (s *simulator) createNextPod() error {
	pod := s.podGenerator.Generate()

	s.simulated++

	return s.CreatePod(pod)
}
