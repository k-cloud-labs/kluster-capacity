package capacityestimation

import (
	"fmt"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/capacityestimation/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg"
	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type PodGenerator interface {
	Generate() *corev1.Pod
}

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkg.Simulator

	podGenerator PodGenerator
	simulatedPod *corev1.Pod
	maxSimulated int
	simulated    int
}

type multiSimulator struct {
	simulators []*simulator
	reports    pkg.Printer
}

// NewCESimulatorExecutor create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCESimulatorExecutor(conf *options.CapacityEstimationConfig) (pkg.SimulatorExecutor, error) {
	newSimulator := func(pod *corev1.Pod) (*simulator, error) {
		kubeSchedulerConfig, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig, conf.Options.KubeConfig)
		if err != nil {
			return nil, err
		}

		kubeConfig, err := utils.BuildRestConfig(conf.Options.KubeConfig)
		if err != nil {
			return nil, err
		}

		s := &simulator{
			podGenerator: NewSinglePodGenerator(pod),
			simulatedPod: pod,
			simulated:    0,
			maxSimulated: conf.Options.MaxLimit,
		}

		err = s.addEventHandlers(kubeSchedulerConfig.InformerFactory)
		if err != nil {
			return nil, err
		}

		genericSimulator, err := pkgframework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
			pkgframework.WithExcludeNodes(conf.Options.ExcludeNodes),
			pkgframework.WithPostBindHook(s.postBindHook))
		if err != nil {
			return nil, err
		}

		s.Simulator = genericSimulator

		return s, nil
	}

	ms := &multiSimulator{
		simulators: make([]*simulator, 0),
	}

	for _, pod := range conf.Pods {
		s, err := newSimulator(pod)
		if err != nil {
			return nil, err
		}

		ms.simulators = append(ms.simulators, s)
	}

	return ms, nil
}

func (s *simulator) Initialize(objs ...runtime.Object) error {
	err := s.InitTheWorld(objs...)
	if err != nil {
		return err
	}

	// create first pod
	return s.createNextPod()
}

func (s *simulator) Report() pkg.Printer {
	return generateReport([]*corev1.Pod{s.simulatedPod}, s.Status())
}

func (ms *multiSimulator) Initialize(objs ...runtime.Object) error {
	for _, s := range ms.simulators {
		if err := s.Initialize(objs...); err != nil {
			return err
		}
	}

	return nil
}

func (ms *multiSimulator) Run() error {
	g := errgroup.Group{}
	reports := make(CapacityEstimationReviews, len(ms.simulators))
	for i, s := range ms.simulators {
		i := i
		s := s
		g.Go(func() error {
			err := s.Run()
			if err != nil {
				return err
			}
			reports[i] = s.Report().(*CapacityEstimationReview)
			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return err
	}

	ms.reports = reports

	return nil
}

func (ms *multiSimulator) Report() pkg.Printer {
	return ms.reports
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {
	if !metav1.HasAnnotation(bindPod.ObjectMeta, pkg.PodProvisioner) {
		return nil
	}
	s.UpdateScheduledPods(bindPod)

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
	informerFactory.Core().V1().Pods().Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				if pod, ok := obj.(*corev1.Pod); ok && pod.Spec.SchedulerName == pkg.SchedulerName &&
					metav1.HasAnnotation(pod.ObjectMeta, pkg.PodProvisioner) {
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
