package schedulersimulation

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/schedulersimulation/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type simulator struct {
	pkg.Framework

	exitCondition string
}

func NewSSSimulatorExecutor(conf *options.SchedulerSimulationConfig) (pkg.Simulator, error) {
	kubeSchedulerConfig, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig, conf.Options.KubeConfig)
	if err != nil {
		return nil, err
	}

	kubeConfig, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return nil, err
	}

	framework, err := framework.NewKubeSchedulerFramework(kubeSchedulerConfig, kubeConfig,
		framework.WithNodeImages(false),
		framework.WithScheduledPods(false),
		framework.WithTerminatingPods(false),
		framework.WithExcludeNodes(conf.Options.ExcludeNodes),
		framework.WithSaveTo(conf.Options.SaveTo))
	if err != nil {
		return nil, err
	}

	s := &simulator{
		Framework:     framework,
		exitCondition: conf.Options.ExitCondition,
	}

	err = s.addEventHandlers(kubeSchedulerConfig.InformerFactory)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *simulator) Initialize(objs ...runtime.Object) error {
	return s.InitTheWorld(objs...)
}

func (s *simulator) Report() pkg.Printer {
	return generateReport(s.Status())
}

func (s *simulator) addEventHandlers(informerFactory informers.SharedInformerFactory) (err error) {
	succeedPodMap := sync.Map{}
	failedPodMap := sync.Map{}
	keyFunc := func(pod *corev1.Pod) string {
		return pod.Namespace + "/" + pod.Name
	}
	count := 0
	informerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod := obj.(*corev1.Pod)
			if len(pod.Spec.NodeName) > 0 {
				succeedPodMap.Store(keyFunc(pod), true)
			}
			count++
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod := newObj.(*corev1.Pod)
			key := keyFunc(pod)
			if len(pod.Spec.NodeName) > 0 {
				succeedPodMap.Store(key, true)
				if _, ok := failedPodMap.Load(key); ok {
					failedPodMap.Delete(key)
				}
			} else {
				for _, cond := range pod.Status.Conditions {
					if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
						failedPodMap.Store(key, true)
					}
				}
			}

			var (
				succeedCount int
				failedCount  int
				stop         bool
				reason       string
			)
			succeedPodMap.Range(func(key, value any) bool {
				succeedCount++
				return true
			})
			failedPodMap.Range(func(key, value any) bool {
				failedCount++
				return true
			})

			if s.exitCondition == options.ExitWhenAllScheduled && succeedCount+failedCount == count {
				stop = true
				reason = "AllScheduled: %d pod(s) have been scheduled once."
			} else if s.exitCondition == options.ExitWhenAllSucceed && succeedCount == count {
				stop = true
				reason = "AllSucceed: %d pod(s) have been scheduled successfully."
			}

			if stop {
				err = s.Stop(fmt.Sprintf(reason, count))
			}
		},
	})

	return
}
