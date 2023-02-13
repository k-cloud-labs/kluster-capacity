package schedulersimulation

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/schedulersimulation/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type simulator struct {
	framework.Simulator

	exitCondition string
	saveTo        string
}

func NewSSSimulatorExecutor(schedulerCfg string, kubeCfg string, exitCondition string, saveTo string, excludeNodes []string) (framework.SimulatorExecutor, error) {
	kubeSchedulerConfig, err := utils.BuildKubeSchedulerCompletedConfig(schedulerCfg)
	if err != nil {
		return nil, err
	}

	kubeConfig, err := utils.BuildRestConfig(kubeCfg)
	if err != nil {
		return nil, err
	}

	scheduler, err := framework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
		framework.WithNodeImages(false),
		framework.WithScheduledPods(false),
		framework.WithExcludeNodes(excludeNodes),
		framework.WithSaveTo(saveTo))
	if err != nil {
		return nil, err
	}

	s := &simulator{
		Simulator:     scheduler,
		exitCondition: exitCondition,
		saveTo:        saveTo,
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

func (s *simulator) Report() framework.Printer {
	return generateReport(s.Status())
}

func (s *simulator) addEventHandlers(informerFactory informers.SharedInformerFactory) (err error) {
	succeedPodMap := sync.Map{}
	failedPodMap := sync.Map{}
	count := 0
	_, _ = informerFactory.Core().V1().Pods().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			if count == 0 {
				pods, _ := informerFactory.Core().V1().Pods().Lister().Pods(metav1.NamespaceAll).List(labels.Everything())
				count = len(pods)
			}

			pod := newObj.(*corev1.Pod)
			key := pod.Namespace + "/" + pod.Name
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

			allPods := func() []*corev1.Pod {
				pods, _ := informerFactory.Core().V1().Pods().Lister().Pods(metav1.NamespaceAll).List(labels.Everything())
				return pods
			}

			if s.exitCondition == options.ExitWhenAllScheduled && succeedCount+failedCount == count {
				stop = true
				reason = "AllScheduled: %d pod(s) have been scheduled once."
			} else if s.exitCondition == options.ExitWhenAllSucceed && succeedCount == count {
				stop = true
				reason = "AllSucceed: %d pod(s) have been scheduled successfully."
			}

			if stop {
				pods := allPods()
				s.UpdateStatus(pods...)
				err = s.Stop(fmt.Sprintf(reason, len(pods)))
			}
		},
	})

	return
}
