package framework

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	externalclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/scheduler"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/profile"
)

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	Pods       []*corev1.Pod
	StopReason string
}

type genericSimulator struct {
	externalKubeClient externalclientset.Interface
	informerFactory    informers.SharedInformerFactory
	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory

	// scheduler
	scheduler           *scheduler.Scheduler
	excludeNodes        sets.Set[string]
	outOfTreeRegistry   runtime.Registry
	customBind          config.PluginSet
	customEventHandlers []func()

	// for scheduler and informer
	informerCh  chan struct{}
	schedulerCh chan struct{}

	// for simulator
	stopCh  chan struct{}
	stopMux sync.Mutex
	stopped bool

	// final status
	status Status
}

type Option func(*genericSimulator)

func WithExcludeNodes(excludeNodes []string) Option {
	return func(s *genericSimulator) {
		s.excludeNodes = sets.New[string](excludeNodes...)
	}
}

func WithOutOfTreeRegistry(registry runtime.Registry) Option {
	return func(s *genericSimulator) {
		s.outOfTreeRegistry = registry
	}
}

func WithCustomBind(plugins config.PluginSet) Option {
	return func(s *genericSimulator) {
		s.customBind = plugins
	}
}

func WithCustomEventHandlers(handlers []func()) Option {
	return func(s *genericSimulator) {
		s.customEventHandlers = handlers
	}
}

// NewGenericSimulator create a generic simulator for ce, cc, ss simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewGenericSimulator(kubeSchedulerConfig *schedconfig.CompletedConfig, kubeConfig *restclient.Config, options ...Option) (Simulator, error) {
	kubeSchedulerConfig.InformerFactory.InformerFor(&corev1.Pod{}, newPodInformer)

	// create internal namespace for simulated pod
	_, err := kubeSchedulerConfig.Client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: Namespace,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	s := &genericSimulator{
		externalKubeClient: kubeSchedulerConfig.Client,
		stopCh:             make(chan struct{}),
		informerFactory:    kubeSchedulerConfig.InformerFactory,
		informerCh:         make(chan struct{}),
		schedulerCh:        make(chan struct{}),
	}
	for _, option := range options {
		option(s)
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

func (s *genericSimulator) UpdateStatus(pod *corev1.Pod) {
	s.status.Pods = append(s.status.Pods, pod)
}

func (s *genericSimulator) Status() Status {
	return s.status
}

func (s *genericSimulator) Stop(reason string) {
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

func (s *genericSimulator) CreatePod(pod *corev1.Pod) error {
	_, err := s.externalKubeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func (s *genericSimulator) Run() error {
	ctx, cancel := context.WithCancel(context.Background())

	// wait for all informer cache synced
	s.informerFactory.WaitForCacheSync(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.WaitForCacheSync(s.informerCh)
	}
	go s.scheduler.Run(ctx)

	<-s.stopCh
	cancel()

	return nil
}

func (s *genericSimulator) InitializeWithClient(client clientset.Interface) error {
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

func (s *genericSimulator) InitializeWithInformerFactory(factory informers.SharedInformerFactory) error {
	return errors.New("not implemented yet")
}

func (s *genericSimulator) createScheduler(cc *schedconfig.CompletedConfig) (*scheduler.Scheduler, error) {
	// custom event handlers
	for _, handler := range s.customEventHandlers {
		handler()
	}

	// custom bind plugin
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled, s.customBind.Enabled...)
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled, s.customBind.Disabled...)

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
		scheduler.WithFrameworkOutOfTreeRegistry(s.outOfTreeRegistry),
		scheduler.WithPodMaxBackoffSeconds(cc.ComponentConfig.PodMaxBackoffSeconds),
		scheduler.WithPodInitialBackoffSeconds(cc.ComponentConfig.PodInitialBackoffSeconds),
		scheduler.WithExtenders(cc.ComponentConfig.Extenders...),
		scheduler.WithParallelism(cc.ComponentConfig.Parallelism),
	)
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
