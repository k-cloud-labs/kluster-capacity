package schedulersimulation

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/schedulersimulation/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework"
)

type simulator struct {
	framework.Simulator

	exitCondition string
}

func NewSSSimulatorExecutor(kubeSchedulerConfig *schedconfig.CompletedConfig, kubeConfig *restclient.Config, exitCondition string, excludeNodes []string) (framework.SimulatorExecutor, error) {
	scheduler, err := framework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
		framework.WithNodeImages(false),
		framework.WithScheduledPods(false),
		framework.WithExcludeNodes(excludeNodes))
	if err != nil {
		return nil, err
	}

	s := &simulator{
		Simulator:     scheduler,
		exitCondition: exitCondition,
	}

	s.addEventHandlers(kubeSchedulerConfig.InformerFactory)

	return s, nil
}

func (s *simulator) Initialize() error {
	return s.InitTheWorld()
}

func (s *simulator) Report() framework.Printer {
	return generateReport(s.Status())
}

func (s *simulator) addEventHandlers(informerFactory informers.SharedInformerFactory) {
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
				reason = "AllScheduled: %s pod(s) have been scheduled once."
			} else if s.exitCondition == options.ExitWhenAllSucceed && succeedCount == count {
				stop = true
				reason = "AllSucceed: %s pod(s) have been scheduled successfully."
			}

			if stop {
				pods := allPods()
				s.UpdateStatus(pods...)
				s.Stop(fmt.Sprintf(reason, len(pods)))
			}
		},
	})
}
