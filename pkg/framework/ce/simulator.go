package ce

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	externalclientset "k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/scheduler"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/profile"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/framework/plugins/capacityestimationbinder"
)

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	externalKubeClient externalclientset.Interface
	informerFactory    informers.SharedInformerFactory
	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory

	// schedulers
	scheduler *scheduler.Scheduler

	// pod to schedule
	podGenerator PodGenerator
	simulatedPod *corev1.Pod
	maxSimulated int
	simulated    int
	status       Status
	excludeNodes sets.Set[string]

	// for scheduler and informer
	informerCh  chan struct{}
	schedulerCh chan struct{}

	// for simulator
	stopCh  chan struct{}
	stopMux sync.Mutex
	stopped bool
}

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	Pods       []*corev1.Pod
	StopReason string
}

// NewCESimulator create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCESimulator(kubeSchedulerConfig *schedconfig.CompletedConfig, kubeConfig *restclient.Config, simulatedPod *corev1.Pod, maxPods int, excludeNodes []string) (pkgframework.Simulator, error) {
	client := fakeclientset.NewSimpleClientset()
	sharedInformerFactory := informers.NewSharedInformerFactory(client, 0)
	sharedInformerFactory.InformerFor(&corev1.Pod{}, newPodInformer)

	// create internal namespace for simulated pod
	_, err := client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: pkgframework.Namespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	kubeSchedulerConfig.Client = client

	s := &simulator{
		externalKubeClient: client,
		podGenerator:       NewSinglePodGenerator(simulatedPod),
		simulatedPod:       simulatedPod,
		simulated:          0,
		maxSimulated:       maxPods,
		stopCh:             make(chan struct{}),
		informerFactory:    sharedInformerFactory,
		informerCh:         make(chan struct{}),
		schedulerCh:        make(chan struct{}),
		excludeNodes:       sets.New[string](excludeNodes...),
	}

	// only for latest k8s version
	if kubeConfig != nil {
		dynClient := dynamic.NewForConfigOrDie(kubeConfig)
		s.dynInformerFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0, corev1.NamespaceAll, nil)
	}

	scheduler, err := s.createScheduler(kubeSchedulerConfig)
	if err != nil {
		return nil, err
	}

	s.scheduler = scheduler

	s.informerFactory.Start(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.Start(s.informerCh)
	}

	return s, nil
}

func (s *simulator) Run() error {
	ctx, cancel := context.WithCancel(context.Background())

	// wait for all informer cache synced
	s.informerFactory.WaitForCacheSync(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.WaitForCacheSync(s.informerCh)
	}
	go s.scheduler.Run(ctx)

	// create the first simulated pod
	err := s.createNextPod()
	if err != nil {
		cancel()
		s.stop(fmt.Sprintf("CreateFirstPod(Unexpected): %s", err.Error()))
		return err
	}
	<-s.stopCh
	cancel()

	return nil
}

func (s *simulator) SyncWithClient(client clientset.Interface) error {
	listOptions := metav1.ListOptions{ResourceVersion: "0"}
	createOptions := metav1.CreateOptions{}

	nsItems, err := client.CoreV1().Namespaces().List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list ns: %v", err)
	}

	for _, item := range nsItems.Items {
		if _, err := s.externalKubeClient.CoreV1().Namespaces().Create(context.TODO(), &item, createOptions); err != nil {
			return fmt.Errorf("unable to copy ns: %v", err)
		}
	}

	podItems, err := client.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list pods: %v", err)
	}

	for _, item := range podItems.Items {
		// selector := fmt.Sprintf("status.phase!=%v,status.phase!=%v", v1.PodSucceeded, v1.PodFailed)
		// field selector are not supported by fake clientset/informers
		if item.Status.Phase != corev1.PodSucceeded && item.Status.Phase != corev1.PodFailed {
			if _, err := s.externalKubeClient.CoreV1().Pods(item.Namespace).Create(context.TODO(), &item, createOptions); err != nil {
				return fmt.Errorf("unable to copy pod: %v", err)
			}
		}
	}

	nodeItems, err := client.CoreV1().Nodes().List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list nodes: %v", err)
	}

	for _, item := range nodeItems.Items {
		if s.excludeNodes.Has(item.Name) {
			continue
		}
		if _, err := s.externalKubeClient.CoreV1().Nodes().Create(context.TODO(), &item, createOptions); err != nil {
			return fmt.Errorf("unable to copy node: %v", err)
		}
	}

	pvcItems, err := client.CoreV1().PersistentVolumeClaims(metav1.NamespaceAll).List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list pvcs: %v", err)
	}

	for _, item := range pvcItems.Items {
		if _, err := s.externalKubeClient.CoreV1().PersistentVolumeClaims(item.Namespace).Create(context.TODO(), &item, createOptions); err != nil {
			return fmt.Errorf("unable to copy pvc: %v", err)
		}
	}

	pvItems, err := client.CoreV1().PersistentVolumes().List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list pvcs: %v", err)
	}

	for _, item := range pvItems.Items {
		if _, err := s.externalKubeClient.CoreV1().PersistentVolumes().Create(context.TODO(), &item, createOptions); err != nil {
			return fmt.Errorf("unable to copy pv: %v", err)
		}
	}

	storageClassesItems, err := client.StorageV1().StorageClasses().List(context.TODO(), listOptions)
	if err != nil {
		return fmt.Errorf("unable to list storage classes: %v", err)
	}

	for _, item := range storageClassesItems.Items {
		if _, err := s.externalKubeClient.StorageV1().StorageClasses().Create(context.TODO(), &item, createOptions); err != nil {
			return fmt.Errorf("unable to copy storage class: %v", err)
		}
	}

	return nil
}

func (s *simulator) SyncWithInformerFactory(factory informers.SharedInformerFactory) error {
	return errors.New("not implemented yet")
}

func (s *simulator) Report() pkgframework.Printer {
	return generateReport([]*corev1.Pod{s.simulatedPod}, s.status)
}

func (s *simulator) createScheduler(cc *schedconfig.CompletedConfig) (*scheduler.Scheduler, error) {
	// TODO: support custom outOfTreeRegistry
	outOfTreeRegistry := frameworkruntime.Registry{
		capacityestimationbinder.Name: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
			return capacityestimationbinder.New(s.externalKubeClient, configuration, f, s.postBindHook)
		},
	}

	// stop the simulator if necessary
	// stop condition: reach limit and failed schedule
	_, err := s.informerFactory.Core().V1().Pods().Informer().AddEventHandler(
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
								s.stop(fmt.Sprintf("%v: %v", podCondition.Reason, podCondition.Message))
							}
						}
					}
				},
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// create the scheduler.
	return scheduler.New(
		s.externalKubeClient,
		s.informerFactory,
		s.dynInformerFactory,
		getRecorderFactory(cc),
		s.schedulerCh,
		scheduler.WithComponentConfigVersion(cc.ComponentConfig.TypeMeta.APIVersion),
		scheduler.WithKubeConfig(cc.KubeConfig),
		scheduler.WithProfiles(cc.ComponentConfig.Profiles...),
		scheduler.WithPercentageOfNodesToScore(cc.ComponentConfig.PercentageOfNodesToScore),
		scheduler.WithFrameworkOutOfTreeRegistry(outOfTreeRegistry),
		scheduler.WithPodMaxBackoffSeconds(cc.ComponentConfig.PodMaxBackoffSeconds),
		scheduler.WithPodInitialBackoffSeconds(cc.ComponentConfig.PodInitialBackoffSeconds),
		scheduler.WithExtenders(cc.ComponentConfig.Extenders...),
		scheduler.WithParallelism(cc.ComponentConfig.Parallelism),
	)
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {
	s.status.Pods = append(s.status.Pods, bindPod)

	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		s.stop(fmt.Sprintf("LimitReached: Maximum number of pods simulated: %v", s.maxSimulated))
		return nil
	}

	if err := s.createNextPod(); err != nil {
		return fmt.Errorf("unable to create next pod for simulated scheduling: %v", err)
	}
	return nil
}

func (s *simulator) stop(reason string) {
	s.stopMux.Lock()
	defer s.stopMux.Unlock()

	if s.stopped {
		return
	}

	s.status.StopReason = reason
	s.stopped = true
	close(s.informerCh)
	close(s.schedulerCh)
	close(s.stopCh)
}

func (s *simulator) createNextPod() error {
	pod := s.podGenerator.Generate()

	s.simulated++

	_, err := s.externalKubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func newPodInformer(cs externalclientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	selector := fmt.Sprintf("status.phase!=%v,status.phase!=%v", corev1.PodSucceeded, corev1.PodFailed)
	tweakListOptions := func(options *metav1.ListOptions) {
		options.FieldSelector = selector
	}
	return coreinformers.NewFilteredPodInformer(cs, metav1.NamespaceAll, resyncPeriod, nil, tweakListOptions)
}

func getRecorderFactory(cc *schedconfig.CompletedConfig) profile.RecorderFactory {
	return func(name string) events.EventRecorder {
		return cc.EventBroadcaster.NewRecorder(name)
	}
}
