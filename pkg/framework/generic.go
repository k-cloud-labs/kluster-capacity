package framework

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1alpha1 "k8s.io/api/resource/v1alpha1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/profile"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/k-cloud-labs/kluster-capacity/pkg/plugins/generic"
)

func init() {
	if err := corev1.AddToScheme(legacyscheme.Scheme); err != nil {
		fmt.Printf("err: %v\n", err)
	}
	// add your own scheme here to use dynamic informer factory when you have some custom filter plugins
	// which uses other resources than defined in scheduler.
	// for details, refer to k8s.io/kubernetes/pkg/scheduler/eventhandlers.go
}

var (
	initResources = map[schema.GroupVersionKind]func() runtime.Object{
		corev1.SchemeGroupVersion.WithKind("Pod"):                   func() runtime.Object { return &corev1.Pod{} },
		corev1.SchemeGroupVersion.WithKind("Node"):                  func() runtime.Object { return &corev1.Node{} },
		corev1.SchemeGroupVersion.WithKind("PersistentVolume"):      func() runtime.Object { return &corev1.PersistentVolume{} },
		corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim"): func() runtime.Object { return &corev1.PersistentVolumeClaim{} },
		storagev1.SchemeGroupVersion.WithKind("StorageClass"):       func() runtime.Object { return &storagev1.StorageClass{} },
		storagev1.SchemeGroupVersion.WithKind("CSINode"):            func() runtime.Object { return &storagev1.CSINode{} },
		storagev1.SchemeGroupVersion.WithKind("CSIDriver"):          func() runtime.Object { return &storagev1.CSIDriver{} },
		storagev1.SchemeGroupVersion.WithKind("CSIStorageCapacity"): func() runtime.Object { return &storagev1.CSIStorageCapacity{} },
		resourcev1alpha1.SchemeGroupVersion.WithKind("PodScheduling"): func() runtime.Object {
			if utilfeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation) {
				return &resourcev1alpha1.PodScheduling{}
			}

			return nil
		},
		resourcev1alpha1.SchemeGroupVersion.WithKind("ResourceClaim"): func() runtime.Object {
			if utilfeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation) {
				return &resourcev1alpha1.ResourceClaim{}
			}

			return nil
		},
	}
	once        sync.Once
	initObjects []runtime.Object
)

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	Pods       []*corev1.Pod
	StopReason string
}

type genericSimulator struct {
	// fake clientset used by scheduler
	fakeClient clientset.Interface
	// fake informer factory used by scheduler
	fakeInformerFactory informers.SharedInformerFactory
	// TODO: follow kubernetes master branch code
	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory
	restMapper         meta.RESTMapper

	// scheduler
	scheduler           *scheduler.Scheduler
	excludeNodes        sets.Set[string]
	outOfTreeRegistry   frameworkruntime.Registry
	customBind          kubeschedulerconfig.PluginSet
	customPreBind       kubeschedulerconfig.PluginSet
	customPostBind      kubeschedulerconfig.PluginSet
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

func WithOutOfTreeRegistry(registry frameworkruntime.Registry) Option {
	return func(s *genericSimulator) {
		s.outOfTreeRegistry = registry
	}
}

func WithCustomBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *genericSimulator) {
		s.customBind = plugins
	}
}

func WithCustomPreBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *genericSimulator) {
		s.customPreBind = plugins
	}
}

func WithCustomPostBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *genericSimulator) {
		s.customPostBind = plugins
	}
}

func WithCustomEventHandlers(handlers []func()) Option {
	return func(s *genericSimulator) {
		s.customEventHandlers = handlers
	}
}

// NewGenericSimulator create a generic simulator for ce, cc, ss simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewGenericSimulator(kubeSchedulerConfig *schedconfig.CompletedConfig, restConfig *restclient.Config, options ...Option) (Simulator, error) {
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

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)
	restMapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	if err != nil {
		return nil, err
	}

	// black magic
	initObjects := getInitObjects(restMapper, dynamicClient)
	for _, unstructuredObj := range initObjects {
		obj := initResources[unstructuredObj.GetObjectKind().GroupVersionKind()]()
		runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.(*unstructured.Unstructured).UnstructuredContent(), obj)
		if err := kubeSchedulerConfig.Client.(testing.FakeClient).Tracker().Add(obj); err != nil {
			return nil, err
		}
	}

	s := &genericSimulator{
		fakeClient:          kubeSchedulerConfig.Client,
		restMapper:          restMapper,
		stopCh:              make(chan struct{}),
		fakeInformerFactory: kubeSchedulerConfig.InformerFactory,
		informerCh:          make(chan struct{}),
		schedulerCh:         make(chan struct{}),
	}
	for _, option := range options {
		option(s)
	}

	// only for latest k8s version
	if restConfig != nil {
		dynClient := dynamic.NewForConfigOrDie(restConfig)
		s.dynInformerFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0, corev1.NamespaceAll, nil)
	}

	scheduler, err := s.createScheduler(kubeSchedulerConfig)
	if err != nil {
		return nil, err
	}

	s.scheduler = scheduler

	s.fakeInformerFactory.Start(s.informerCh)
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
	_, err := s.fakeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func (s *genericSimulator) Run() error {
	ctx, cancel := context.WithCancel(context.Background())

	// wait for all informer cache synced
	s.fakeInformerFactory.WaitForCacheSync(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.WaitForCacheSync(s.informerCh)
	}
	go s.scheduler.Run(ctx)

	<-s.stopCh
	cancel()

	return nil
}

func (s *genericSimulator) createScheduler(cc *schedconfig.CompletedConfig) (*scheduler.Scheduler, error) {
	// custom event handlers
	for _, handler := range s.customEventHandlers {
		handler()
	}

	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Enabled = []kubeschedulerconfig.Plugin{{Name: generic.Name}}
	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Disabled = []kubeschedulerconfig.Plugin{{Name: volumebinding.Name}}
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled = []kubeschedulerconfig.Plugin{{Name: generic.Name}}
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled = []kubeschedulerconfig.Plugin{{Name: defaultbinder.Name}}

	// custom bind plugin
	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.PreBind.Enabled, s.customPreBind.Enabled...)
	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.PreBind.Disabled, s.customPreBind.Disabled...)
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled, s.customBind.Enabled...)
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled, s.customBind.Disabled...)
	cc.ComponentConfig.Profiles[0].Plugins.PostBind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.PostBind.Enabled, s.customPostBind.Enabled...)
	cc.ComponentConfig.Profiles[0].Plugins.PostBind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.PostBind.Disabled, s.customPostBind.Disabled...)

	// create the scheduler.
	return scheduler.New(
		s.fakeClient,
		s.fakeInformerFactory,
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

func newPodInformer(cs clientset.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
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

// getInitObjects return all objects need to add to scheduler.
// it's pkg scope for multi scheduler to avoid calling too much times of real kube-apiserver
func getInitObjects(restMapper meta.RESTMapper, dynClient dynamic.Interface) []runtime.Object {
	once.Do(func() {
		// each item is UnstructuredList
		for gvk, _ := range initResources {
			restMapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
			if err != nil && !meta.IsNoMatchError(err) {
				fmt.Printf("unable to get rest mapping for %s, error: %s", gvk.String(), err.Error())
				os.Exit(1)
			}

			if restMapping != nil {
				var (
					list *unstructured.UnstructuredList
					err  error
				)
				if restMapping.Scope.Name() == meta.RESTScopeNameRoot {
					list, err = dynClient.Resource(restMapping.Resource).List(context.TODO(), metav1.ListOptions{ResourceVersion: "0"})
					if err != nil && !apierrors.IsNotFound(err) {
						fmt.Printf("unable to list %s, error: %s", gvk.String(), err.Error())
						os.Exit(1)
					}
				} else {
					list, err = dynClient.Resource(restMapping.Resource).Namespace(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{ResourceVersion: "0"})
					if err != nil && !apierrors.IsNotFound(err) {
						fmt.Printf("unable to list %s, error: %s", gvk.String(), err.Error())
						os.Exit(1)
					}
				}

				list.EachListItem(func(object runtime.Object) error {
					initObjects = append(initObjects, object)
					return nil
				})
			}
		}
	})

	return initObjects
}
