package clustercompression

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	uuid "github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	apiv1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	frameworkruntime "k8s.io/kubernetes/pkg/scheduler/framework/runtime"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/plugins/capacityestimation"
)

const (
	Podsuffix = "-simulator-"
)

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkgframework.Simulator

	maxSimulated int
	simulated    int
	fakeClient   clientset.Interface
	createPods   []*corev1.Pod
	nowNode      string
	mux          sync.Mutex
	nodeFilter   NodeFilter
}

// NewCCSimulatorExecutor create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCCSimulatorExecutor(kubeSchedulerConfig *schedconfig.CompletedConfig, kubeConfig *restclient.Config, maxNodes int, excludeNodes []string, skipTaintNode, skipNotReadyNode bool) (pkgframework.SimulatorExecutor, error) {
	s := &simulator{
		simulated:    0,
		createPods:   []*corev1.Pod{},
		maxSimulated: maxNodes,
	}

	genericSimulator, err := pkgframework.NewGenericSimulator(kubeSchedulerConfig, kubeConfig,
		pkgframework.WithExcludeNodes(excludeNodes),
		// add your custom plugins
		pkgframework.WithOutOfTreeRegistry(frameworkruntime.Registry{
			capacityestimation.Name: func(configuration runtime.Object, f framework.Handle) (framework.Plugin, error) {
				return capacityestimation.New(s.postBindHook)
			},
		}),
		// plugin configs added here is only for simulator logic.
		// custom plugin configs of real scheduler is in your kubescheduler config file.
		pkgframework.WithCustomPostBind(config.PluginSet{
			Enabled: []config.Plugin{{Name: capacityestimation.Name}},
		}),
		// add your custom event handlers
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
											// 1. Empty all Pods created by fake before
											// 2. UnCordon this node
											// 3. Type the flags that cannot be filtered, clear the flags that prohibit scheduling, and skip to selectNextNode
											klog.Warningf("the pod :%s, Reason:%s, Message:%s\n", pod.Namespace+"/"+pod.Name, podCondition.Reason, podCondition.Message)
											_ = s.DeletePodsFromCreatePods()
											_ = s.UnCordonNode(s.nowNode)
											_ = s.AddLabelToNode(s.nowNode, NodeSkipperLabel, "true")
											err := s.selectNextNode()
											if err != nil {
												s.Stop("FailedSelectorNode: Could not find a node that satisfies the condition")
											}
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
	s.fakeClient = genericSimulator.GetClient()
	s.nodeFilter = NewNodeFilter(s.fakeClient, skipTaintNode, skipNotReadyNode, excludeNodes)

	return s, nil
}

func (s *simulator) Initialize(objs ...runtime.Object) error {
	err := s.Simulator.InitTheWorld(objs...)
	if err != nil {
		return err
	}

	// select first node
	return s.selectNextNode()
}

func (s *simulator) Report() pkgframework.Printer {
	klog.Infof("the following nodes can be offline to save resources: %v", s.Status().NodeNameList)
	klog.Infof("the clusterCompression StopReason: %s", s.Status().StopReason)
	return generateReport(s.Status())
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {
	klog.Infof("scheduled %s pod, pod.scheduler:%s", bindPod.Namespace+"/"+bindPod.Name, bindPod.Spec.SchedulerName)
	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		s.Simulator.Stop(fmt.Sprintf("LimitReached: Maximum number of nodes simulated: %v", s.maxSimulated))
		return nil
	}

	var flag bool
	podList := s.GetCreatePods()

	for i := range podList {
		p, err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Get(context.TODO(), podList[i].Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if p.Spec.NodeName == "" {
			flag = true
		}
	}

	if !flag {
		if s.nowNode != "" {
			s.Simulator.UpdateStatusNode(s.nowNode)
			klog.Infof("add node %s to simulator status", s.nowNode)
			s.nowNode = ""
		}
		s.simulated++
		err := s.selectNextNode()
		if err != nil {
			s.Simulator.Stop("FailedSelectorNode: Could not find a node that satisfies the condition")
			return nil
		}
	}

	return nil
}

func (s *simulator) selectNextNode() error {
	node := s.nodeFilter.SelectNode()
	if node == nil {
		return errors.New("there is no node that can be offline in the cluster, and the program ends")
	}

	klog.Infof("select %s node to simulator\n", node.Name)
	s.CordonNode(node)

	err := s.CreatePodsByNode(node)
	if err != nil {
		s.HandleFailedCreatePod(node.Name)
		return nil
	}

	s.nowNode = node.Name
	return nil
}

func (s *simulator) HandleFailedCreatePod(nodeName string) {
	s.UnCordonNode(nodeName)
	s.DeletePodsFromCreatePods()
}

func (s *simulator) GetCreatePods() []*corev1.Pod {
	s.mux.Lock()
	defer s.mux.Unlock()
	return s.createPods
}

func (s *simulator) SetCreatePods(podList []*corev1.Pod) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.createPods = podList
}

func (s *simulator) CordonNode(node *corev1.Node) error {
	node, err := s.fakeClient.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	copy := node.DeepCopy()

	taints := []corev1.Taint{}
	unScheduleTaint := corev1.Taint{
		Key:    "node.kubernetes.io/unschedulable",
		Effect: corev1.TaintEffectNoSchedule,
	}
	taints = append(taints, unScheduleTaint)

	for i := range copy.Spec.Taints {
		if copy.Spec.Taints[i].Key != "node.kubernetes.io/unschedulable" {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.Infof("CordonNode node %s success\n", node.Name)
	return nil
}

func (s *simulator) UnCordonNode(nodeName string) error {
	node, err := s.fakeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	copy := node.DeepCopy()

	taints := []corev1.Taint{}
	for i := range copy.Spec.Taints {
		if copy.Spec.Taints[i].Key != "node.kubernetes.io/unschedulable" {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.Infof("UnCordonNode node %s success\n", nodeName)
	return nil

}

func (s *simulator) AddLabelToNode(nodeName string, labelKey string, labelValue string) error {
	node, err := s.fakeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	copy := node.DeepCopy()

	copy.Labels[labelKey] = labelValue
	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.Infof("Add node %s  skipper label success\n", node.Name)
	return nil
}

func (s *simulator) DeletePodsFromCreatePods() error {
	var e error

	podList := s.GetCreatePods()
	for i := range podList {
		err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Delete(context.TODO(), podList[i].Name, metav1.DeleteOptions{})
		if err != nil {
			e = err
			continue
		}
	}
	return e
}

func (s *simulator) CreatePodsByNode(node *corev1.Node) error {
	podList, err := s.getPodsByNode(node)
	if err != nil {
		return err
	}

	podArray := []*corev1.Pod{}
	for i := range podList {
		if podList[i].Status.Phase != corev1.PodSucceeded && podList[i].Status.Phase != corev1.PodFailed &&
			(len(podList[i].OwnerReferences) == 0 || podList[i].OwnerReferences[0].Kind != "DaemonSet") {
			podArray = append(podArray, initPod(podList[i]))
		}
	}

	var createPods []*corev1.Pod
	for i := range podArray {
		newPod, err := s.fakeClient.CoreV1().Pods(podArray[i].Namespace).Create(context.TODO(), podArray[i], metav1.CreateOptions{})
		if err != nil {
			s.SetCreatePods(createPods)
			return err
		}
		createPods = append(createPods, newPod)
	}
	s.SetCreatePods(createPods)

	var nodeStrings []string
	for _, v := range s.GetCreatePods() {
		nodeStrings = append(nodeStrings, v.Namespace+"/"+v.Name)
	}
	klog.Infof("create %d pods, detail podList:%v\n", len(nodeStrings), nodeStrings)

	return nil
}

func (s *simulator) getPodsByNode(node *corev1.Node) ([]corev1.Pod, error) {
	var res []corev1.Pod

	podList, err := s.fakeClient.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{FieldSelector: fmt.Sprintf("%s=%s", "spec.nodeName", node.Name)})
	if err != nil {
		return nil, err
	}

	for i := range podList.Items {
		if podList.Items[i].Spec.NodeName == node.Name {
			res = append(res, podList.Items[i])
		}
	}
	klog.Infof("the node %s ,have %d pods\n", node.Name, len(res))
	return res, nil
}

func initPod(input corev1.Pod) *corev1.Pod {
	podSpec := input.Spec.DeepCopy()
	pod := &corev1.Pod{
		Spec: *podSpec,
	}
	apiv1.SetObjectDefaults_Pod(pod)

	// reset pod
	pod.Spec.NodeName = ""
	pod.Spec.SchedulerName = pkgframework.SchedulerName
	pod.Status = corev1.PodStatus{}

	// use simulated pod name with an index to construct the name
	podName := generatorPodName(input.Name)
	pod.ObjectMeta.Name = podName
	pod.ObjectMeta.Namespace = input.Namespace
	pod.ObjectMeta.UID = types.UID(uuid.NewV4().String())
	// Add pod provisioner annotation
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}
	pod.ObjectMeta.Annotations[pkgframework.PodProvisioner] = pkgframework.SchedulerName

	return pod
}

func generatorPodName(podName string) string {
	var res string
	b := strings.Contains(podName, Podsuffix)
	if b {
		sli := strings.Split(podName, Podsuffix)
		if sli[1] == "" {
			res = podName + Podsuffix + "1"
		} else {
			resInt, err := strconv.Atoi(sli[1])
			if err == nil {
				resInt = resInt + 1
				res = sli[0] + Podsuffix + strconv.Itoa(resInt)
			} else {
				res = podName + Podsuffix + "1"
			}
		}
	} else {
		res = podName + Podsuffix + "1"
	}
	return res
}
