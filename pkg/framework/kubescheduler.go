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

package framework

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
	"k8s.io/klog/v2"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultpreemption"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"
	"k8s.io/kubernetes/pkg/scheduler/profile"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
	"github.com/k-cloud-labs/kluster-capacity/pkg/plugins/generic"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
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
		corev1.SchemeGroupVersion.WithKind("Namespace"):             func() runtime.Object { return &corev1.Namespace{} },
		corev1.SchemeGroupVersion.WithKind("Pod"):                   func() runtime.Object { return &corev1.Pod{} },
		corev1.SchemeGroupVersion.WithKind("Node"):                  func() runtime.Object { return &corev1.Node{} },
		corev1.SchemeGroupVersion.WithKind("PersistentVolume"):      func() runtime.Object { return &corev1.PersistentVolume{} },
		corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim"): func() runtime.Object { return &corev1.PersistentVolumeClaim{} },
		corev1.SchemeGroupVersion.WithKind("Service"):               func() runtime.Object { return &corev1.Service{} },
		corev1.SchemeGroupVersion.WithKind("ReplicationController"): func() runtime.Object { return &corev1.ReplicationController{} },
		appsv1.SchemeGroupVersion.WithKind("StatefulSet"):           func() runtime.Object { return &appsv1.StatefulSet{} },
		appsv1.SchemeGroupVersion.WithKind("ReplicaSet"):            func() runtime.Object { return &appsv1.ReplicaSet{} },
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

type kubeschedulerFramework struct {
	// fake clientset used by scheduler
	fakeClient clientset.Interface
	// fake informer factory used by scheduler
	fakeInformerFactory informers.SharedInformerFactory
	// TODO: follow kubernetes master branch code
	dynInformerFactory dynamicinformer.DynamicSharedInformerFactory
	restMapper         meta.RESTMapper
	// real dynamic client to init the world
	dynamicClient *dynamic.DynamicClient

	// scheduler
	scheduler                *scheduler.Scheduler
	excludeNodes             sets.Set[string]
	withScheduledPods        bool
	withNodeImages           bool
	ignorePodsOnExcludesNode bool
	// deletionTimestamp is not nil and phase is not succeed or failed
	withTerminatingPods bool
	outOfTreeRegistry   frameworkruntime.Registry
	customBind          kubeschedulerconfig.PluginSet
	customPreBind       kubeschedulerconfig.PluginSet
	customPostBind      kubeschedulerconfig.PluginSet
	customEventHandlers []func()
	postBindHook        func(*corev1.Pod) error

	// for scheduler and informer
	informerCh  chan struct{}
	schedulerCh chan struct{}

	// for simulator
	stopCh  chan struct{}
	stopMux sync.Mutex
	stopped bool

	// final status
	status *pkg.Status
	// save status to this file if specified
	saveTo string
}

type Option func(*kubeschedulerFramework)

func WithExcludeNodes(excludeNodes []string) Option {
	return func(s *kubeschedulerFramework) {
		s.excludeNodes = sets.New[string](excludeNodes...)
	}
}

func WithOutOfTreeRegistry(registry frameworkruntime.Registry) Option {
	return func(s *kubeschedulerFramework) {
		s.outOfTreeRegistry = registry
	}
}

func WithCustomBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *kubeschedulerFramework) {
		s.customBind = plugins
	}
}

func WithCustomPreBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *kubeschedulerFramework) {
		s.customPreBind = plugins
	}
}

func WithCustomPostBind(plugins kubeschedulerconfig.PluginSet) Option {
	return func(s *kubeschedulerFramework) {
		s.customPostBind = plugins
	}
}

func WithCustomEventHandlers(handlers []func()) Option {
	return func(s *kubeschedulerFramework) {
		s.customEventHandlers = handlers
	}
}

func WithNodeImages(with bool) Option {
	return func(s *kubeschedulerFramework) {
		s.withNodeImages = with
	}
}

func WithScheduledPods(with bool) Option {
	return func(s *kubeschedulerFramework) {
		s.withScheduledPods = with
	}
}

func WithIgnorePodsOnExcludesNode(with bool) Option {
	return func(s *kubeschedulerFramework) {
		s.ignorePodsOnExcludesNode = with
	}
}

func WithPostBindHook(postBindHook func(*corev1.Pod) error) Option {
	return func(s *kubeschedulerFramework) {
		s.postBindHook = postBindHook
	}
}

func WithSaveTo(to string) Option {
	return func(s *kubeschedulerFramework) {
		s.saveTo = to
	}
}

func WithTerminatingPods(with bool) Option {
	return func(s *kubeschedulerFramework) {
		s.withTerminatingPods = with
	}
}

// NewKubeSchedulerFramework create a generic simulator for ce, cc, ss simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewKubeSchedulerFramework(kubeSchedulerConfig *schedconfig.CompletedConfig, restConfig *restclient.Config, options ...Option) (pkg.Framework, error) {
	kubeSchedulerConfig.InformerFactory.InformerFor(&corev1.Pod{}, newPodInformer)

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)
	restMapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	if err != nil {
		return nil, err
	}

	s := &kubeschedulerFramework{
		fakeClient:               kubeSchedulerConfig.Client,
		dynamicClient:            dynamicClient,
		restMapper:               restMapper,
		stopCh:                   make(chan struct{}),
		fakeInformerFactory:      kubeSchedulerConfig.InformerFactory,
		informerCh:               make(chan struct{}),
		schedulerCh:              make(chan struct{}),
		withScheduledPods:        true,
		ignorePodsOnExcludesNode: false,
		withNodeImages:           true,
		withTerminatingPods:      true,
		status:                   &pkg.Status{},
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

	return s, nil
}

func (s *kubeschedulerFramework) GetPodsByNode(nodeName string) ([]*corev1.Pod, error) {
	dump := s.scheduler.Cache.Dump()
	var res []*corev1.Pod
	if dump != nil && dump.Nodes[nodeName] != nil {
		podInfos := dump.Nodes[nodeName].Pods
		for i := range podInfos {
			if podInfos[i].Pod != nil {
				res = append(res, podInfos[i].Pod)
			}
		}
	}

	if res == nil {
		return nil, errors.New("cannot get pods on the node because dump is nil")
	}
	return res, nil
}

// InitTheWorld use objs outside or default init resources to initialize the scheduler
// the objs outside must be typed object.
func (s *kubeschedulerFramework) Initialize(objs ...runtime.Object) error {
	if len(objs) == 0 {
		// black magic
		klog.V(2).InfoS("Init the world form running cluster")
		initObjects := getInitObjects(s.restMapper, s.dynamicClient)
		for _, unstructuredObj := range initObjects {
			obj := initResources[unstructuredObj.GetObjectKind().GroupVersionKind()]()
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.(*unstructured.Unstructured).UnstructuredContent(), obj); err != nil {
				return err
			}
			if needAdd, obj := s.preAdd(obj); needAdd {
				if err := s.fakeClient.(testing.FakeClient).Tracker().Add(obj); err != nil {
					return err
				}
			}
		}
	} else {
		klog.V(2).InfoS("Init the world form snapshot")
		for _, obj := range objs {
			if _, ok := obj.(runtime.Unstructured); ok {
				return errors.New("type of objs used to init the world must not be unstructured")
			}
			if needAdd, obj := s.preAdd(obj); needAdd {
				if err := s.fakeClient.(testing.FakeClient).Tracker().Add(obj); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (s *kubeschedulerFramework) UpdateEstimationPods(pod ...*corev1.Pod) {
	s.status.PodsForEstimation = append(s.status.PodsForEstimation, pod...)
}

func (s *kubeschedulerFramework) UpdateNodesToScaleDown(nodeName string) {
	s.status.NodesToScaleDown = append(s.status.NodesToScaleDown, nodeName)
}

func (s *kubeschedulerFramework) Status() *pkg.Status {
	return s.status
}

func (s *kubeschedulerFramework) Stop(reason string) error {
	s.stopMux.Lock()
	defer func() {
		s.stopMux.Unlock()
	}()

	if s.stopped {
		return nil
	}

	nodeMap := make(map[string]corev1.Node)
	nodeList, _ := s.fakeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{ResourceVersion: "0"})
	for _, node := range nodeList.Items {
		nodeMap[node.Name] = node
	}
	s.status.Nodes = nodeMap

	podList, _ := s.fakeClient.CoreV1().Pods(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{ResourceVersion: "0"})
	s.status.Pods = podList.Items

	s.status.StopReason = reason

	if len(s.saveTo) > 0 {
		file, err := os.OpenFile(s.saveTo, os.O_CREATE|os.O_RDWR, 0755)
		if err != nil {
			panic(err)
		}
		defer file.Close()

		bytes, err := json.Marshal(s.status)
		if err != nil {
			return err
		}

		_, err = file.Write(bytes)
		if err != nil {
			return err
		}
	}

	defer func() {
		close(s.stopCh)
		close(s.informerCh)
		close(s.schedulerCh)
	}()

	s.stopped = true

	return nil
}

func (s *kubeschedulerFramework) CreatePod(pod *corev1.Pod) error {
	_, err := s.fakeClient.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	return err
}

func (s *kubeschedulerFramework) Run(init func() error) error {
	// wait for all informer cache synced
	s.fakeInformerFactory.Start(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.Start(s.informerCh)
	}
	start := time.Now()

	s.fakeInformerFactory.WaitForCacheSync(s.informerCh)
	if s.dynInformerFactory != nil {
		s.dynInformerFactory.WaitForCacheSync(s.informerCh)
	}

	klog.V(4).InfoS("wait sync", "cost", time.Since(start).Milliseconds())

	if init != nil {
		err := init()
		if err != nil {
			return s.Stop(fmt.Sprintf("%s: %s", "FailedRunInit: ", err.Error()))
		}
	}

	go s.scheduler.Run(context.TODO())

	<-s.stopCh

	return nil
}

func (s *kubeschedulerFramework) createScheduler(cc *schedconfig.CompletedConfig) (*scheduler.Scheduler, error) {
	// custom event handlers
	for _, handler := range s.customEventHandlers {
		handler()
	}

	// register default generic plugin
	if s.outOfTreeRegistry == nil {
		s.outOfTreeRegistry = make(frameworkruntime.Registry)
	}
	err := s.outOfTreeRegistry.Register(generic.Name, func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
		return generic.New(s.postBindHook, s.fakeClient, s.status)
	})
	if err != nil {
		return nil, err
	}

	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.PreBind.Enabled, kubeschedulerconfig.Plugin{Name: generic.Name})
	cc.ComponentConfig.Profiles[0].Plugins.PreBind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.PreBind.Disabled, kubeschedulerconfig.Plugin{Name: volumebinding.Name})
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Enabled, kubeschedulerconfig.Plugin{Name: generic.Name})
	cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.Bind.Disabled, kubeschedulerconfig.Plugin{Name: defaultbinder.Name})
	cc.ComponentConfig.Profiles[0].Plugins.PostBind.Enabled = append(cc.ComponentConfig.Profiles[0].Plugins.PostBind.Enabled, kubeschedulerconfig.Plugin{Name: generic.Name})
	cc.ComponentConfig.Profiles[0].Plugins.PostFilter.Disabled = append(cc.ComponentConfig.Profiles[0].Plugins.PostFilter.Disabled, kubeschedulerconfig.Plugin{Name: defaultpreemption.Name})

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

func (s *kubeschedulerFramework) preAdd(obj runtime.Object) (bool, runtime.Object) {
	// filter exclude nodes and pods and update pod, node spec and status property
	if pod, ok := obj.(*corev1.Pod); ok {
		// ignore ds pods on exclude nodes
		if s.excludeNodes != nil {
			if _, ok := s.excludeNodes[pod.Spec.NodeName]; ok {
				if s.ignorePodsOnExcludesNode || pod.OwnerReferences != nil && utils.IsDaemonsetPod(pod.OwnerReferences) {
					return false, nil
				}
			}
		}

		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return false, nil
		}

		if pod.DeletionTimestamp != nil && !s.withTerminatingPods {
			return false, nil
		}

		if !s.withScheduledPods && !utils.IsDaemonsetPod(pod.OwnerReferences) {
			pod := utils.InitPod(pod)
			pod.Status.Phase = corev1.PodPending

			return true, pod
		}
	} else if node, ok := obj.(*corev1.Node); ok && s.excludeNodes != nil {
		if _, ok := s.excludeNodes[node.Name]; ok {
			return false, nil
		} else if !s.withNodeImages {
			node.Status.Images = nil

			return true, node
		}
	}

	return true, obj
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
		for gvk := range initResources {
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
					if restMapping.Resource.Resource == "pods" {
						list, err = dynClient.Resource(restMapping.Resource).Namespace(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{
							ResourceVersion: "0",
							FieldSelector:   fmt.Sprintf("status.phase!=%v,status.phase!=%v", corev1.PodSucceeded, corev1.PodFailed),
						})
					} else {
						list, err = dynClient.Resource(restMapping.Resource).Namespace(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{ResourceVersion: "0"})
					}
					if err != nil && !apierrors.IsNotFound(err) {
						fmt.Printf("unable to list %s, error: %s", gvk.String(), err.Error())
						os.Exit(1)
					}
				}

				_ = list.EachListItem(func(object runtime.Object) error {
					initObjects = append(initObjects, object)
					return nil
				})
			}
		}
	})

	return initObjects
}
