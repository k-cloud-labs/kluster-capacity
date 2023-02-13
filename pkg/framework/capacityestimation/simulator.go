package capacityestimation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type PodGenerator interface {
	Generate() *corev1.Pod
}

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkgframework.Simulator

	podGenerator PodGenerator
	simulatedPod *corev1.Pod
	maxSimulated int
	simulated    int
}

// NewCESimulatorExecutor create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCESimulatorExecutor(simulatedPod *corev1.Pod, schedulerCfg string, kubeCfg string, maxPods int, excludeNodes []string) (pkgframework.SimulatorExecutor, error) {
	kubeSchedulerConfig, err := utils.BuildKubeSchedulerCompletedConfig(schedulerCfg)
	if err != nil {
		return nil, err
	}

	kubeConfig, err := utils.BuildRestConfig(kubeCfg)
	if err != nil {
		return nil, err
	}

	s := &simulator{
		podGenerator: NewSinglePodGenerator(simulatedPod),
		simulatedPod: simulatedPod,
		simulated:    0,
		maxSimulated: maxPods,
	}

	err = s.addEventHandlers(kubeSchedulerConfig.InformerFactory)
	if err != nil {
		return nil, err
	}

	genericSimulator, err := pkgframework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
		pkgframework.WithExcludeNodes(excludeNodes),
		pkgframework.WithPostBindHook(s.postBindHook))
	if err != nil {
		return nil, err
	}

	s.Simulator = genericSimulator

	return s, nil
}

func (s *simulator) Initialize(objs ...runtime.Object) error {
	err := s.InitTheWorld(objs...)
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
	if !metav1.HasAnnotation(bindPod.ObjectMeta, pkgframework.PodProvisioner) {
		return nil
	}
	s.UpdateStatus(bindPod)

	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		return s.Stop(fmt.Sprintf("LimitReached: Maximum number of pods simulated: %v", s.maxSimulated))
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

func (s *simulator) addEventHandlers(informerFactory informers.SharedInformerFactory) (err error) {
	_, _ = informerFactory.Core().V1().Pods().Informer().AddEventHandler(
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
								err = s.Stop(fmt.Sprintf("%v: %v", podCondition.Reason, podCondition.Message))
							}
						}
					}
				},
			},
		},
	)

	return
}
