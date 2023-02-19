package clustercompression

import (
	"context"
	"errors"
	"fmt"
	"sync"

	uuid "github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	apiv1 "k8s.io/kubernetes/pkg/apis/core/v1"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg"
	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

const (
	TaintUnScheduleKey = "node.kubernetes.io/unschedulable"
)

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkg.Simulator

	maxSimulated        int
	simulated           int
	fakeClient          clientset.Interface
	createdPods         []*corev1.Pod
	currentNode         string
	mux                 sync.Mutex
	bindSuccessPodCount int
	nodeFilter          NodeFilter
}

// NewCCSimulatorExecutor create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCCSimulatorExecutor(conf *options.ClusterCompressionConfig) (pkg.SimulatorExecutor, error) {
	cc, err := utils.BuildKubeSchedulerCompletedConfig(conf.Options.SchedulerConfig, conf.Options.KubeConfig)
	if err != nil {
		return nil, err
	}

	kubeConfig, err := utils.BuildRestConfig(conf.Options.KubeConfig)
	if err != nil {
		return nil, err
	}

	s := &simulator{
		simulated:           0,
		bindSuccessPodCount: 0,
		createdPods:         []*corev1.Pod{},
		maxSimulated:        conf.Options.MaxLimit,
	}

	// add your custom event handlers
	err = s.addEventHandlers(cc.InformerFactory)
	if err != nil {
		return nil, err
	}

	genericSimulator, err := pkgframework.NewGenericSimulator(cc, kubeConfig,
		pkgframework.WithExcludeNodes(conf.Options.ExcludeNodes),
		pkgframework.WithPostBindHook(s.postBindHook),
	)
	if err != nil {
		return nil, err
	}

	s.Simulator = genericSimulator
	s.fakeClient = cc.Client
	nodeFilter, err := NewNodeFilter(s, conf.Options.ExcludeNodes, conf.Options.FilterNodeOptions)
	if err != nil {
		return nil, err
	}

	s.nodeFilter = nodeFilter
	return s, nil
}

func (s *simulator) Initialize(objs ...runtime.Object) error {
	err := s.InitTheWorld(objs...)
	if err != nil {
		return err
	}

	// select first node
	return s.selectNextNode()
}

func (s *simulator) Report() pkg.Printer {
	klog.Infof("the following nodes can be offline to save resources: %v", s.Status().ScaleDownNodeNames)
	klog.Infof("the clusterCompression StopReason: %s", s.Status().StopReason)
	return generateReport(s.Status())
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {
	if !metav1.HasAnnotation(bindPod.ObjectMeta, pkg.PodProvisioner) {
		return nil
	}

	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		return s.Stop(fmt.Sprintf("LimitReached: maximum number of nodes simulated: %v", s.maxSimulated))
	}

	s.mux.Lock()
	defer s.mux.Unlock()
	s.bindSuccessPodCount++

	var flag bool
	podList := s.createdPods
	for i := range podList {
		p, err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Get(context.TODO(), podList[i].Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if p.Spec.NodeName == "" {
			flag = true
			break
		}
	}

	if len(podList) > 0 && !flag && s.currentNode != "" {
		if s.bindSuccessPodCount == len(s.createdPods) {
			s.UpdateStatusScaleDownNodeNames(s.currentNode)
			klog.Infof("add node %s to simulator status", s.currentNode)
			s.createdPods = nil
			s.currentNode = ""
			s.simulated++

			err := s.selectNextNode()
			if err != nil {
				return s.Stop("FailedSelectorNode: could not find a node that satisfies the condition")
			}
		}
	}

	return nil
}

func (s *simulator) selectNextNode() error {
	node := s.nodeFilter.SelectNode()
	if node == nil {
		return errors.New("there is no node that can be offline in the cluster")
	}
	klog.Infof("select %s node to simulator\n", node.Name)

	s.bindSuccessPodCount = 0

	err := s.cordon(node)
	if err != nil {
		return err
	}

	err = s.createPodsByNode(node)
	if err != nil {
		return err
	}

	s.currentNode = node.Name
	return nil
}

func (s *simulator) cordon(node *corev1.Node) error {
	node, err := s.fakeClient.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	copy := node.DeepCopy()

	taints := []corev1.Taint{}
	unScheduleTaint := corev1.Taint{
		Key:    TaintUnScheduleKey,
		Effect: corev1.TaintEffectNoSchedule,
	}
	taints = append(taints, unScheduleTaint)

	for i := range copy.Spec.Taints {
		if copy.Spec.Taints[i].Key != TaintUnScheduleKey {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.Infof("cordon node %s successfully\n", node.Name)
	return nil
}

func (s *simulator) unCordon(nodeName string) error {
	node, err := s.fakeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	copy := node.DeepCopy()

	taints := []corev1.Taint{}
	for i := range copy.Spec.Taints {
		if copy.Spec.Taints[i].Key != TaintUnScheduleKey {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.Infof("unCordon node %s successfully\n", nodeName)
	return nil

}

func (s *simulator) addLabelToNode(nodeName string, labelKey string, labelValue string) error {
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
	klog.Infof("add node %s skip label successfully\n", node.Name)
	return nil
}

func (s *simulator) updatePodsFromCreatedPods() error {
	podList := s.createdPods
	for i := range podList {
		err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Delete(context.TODO(), podList[i].Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		_, err = s.fakeClient.CoreV1().Pods(podList[i].Namespace).Create(context.TODO(), podList[i], metav1.CreateOptions{})
		if err != nil {
			return err
		}
	}

	s.createdPods = nil
	return nil
}

func (s *simulator) createPodsByNode(node *corev1.Node) error {
	podList, err := s.getPodsByNode(node)
	if err != nil {
		return err
	}

	var createdPods []*corev1.Pod
	for i := range podList {
		if podList[i].Status.Phase != corev1.PodSucceeded && podList[i].Status.Phase != corev1.PodFailed &&
			(len(podList[i].OwnerReferences) == 0 || podList[i].OwnerReferences[0].Kind != "DaemonSet") {
			createdPods = append(createdPods, podList[i])
			err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Delete(context.TODO(), podList[i].Name, metav1.DeleteOptions{})
			if err != nil {
				return err
			}

			_, err = s.fakeClient.CoreV1().Pods(podList[i].Namespace).Create(context.TODO(), initPod(podList[i]), metav1.CreateOptions{})
			if err != nil {
				return err
			}
		}
	}

	s.createdPods = createdPods
	return nil
}

func (s *simulator) addEventHandlers(informerFactory informers.SharedInformerFactory) (err error) {
	_, _ = informerFactory.Core().V1().Pods().Informer().AddEventHandler(
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
							// Only for pending pods provisioned by cc
							if podCondition.Type == corev1.PodScheduled && podCondition.Status == corev1.ConditionFalse &&
								podCondition.Reason == corev1.PodReasonUnschedulable {
								// 1. Empty all Pods created by fake before
								// 2. unCordon this node
								// 3. Type the flags that cannot be filtered, clear the flags that prohibit scheduling, and skip to selectNextNode
								klog.Warningf("Failed scheduling pod %s, reason: %s, message: %s\n", pod.Namespace+"/"+pod.Name, podCondition.Reason, podCondition.Message)
								err = s.updatePodsFromCreatedPods()
								if err != nil {
									err = s.Stop("FailedDeletePodsFromCreatedPods: " + err.Error())
								}

								if s.currentNode != "" {
									err = s.unCordon(s.currentNode)
									if err != nil {
										err = s.Stop("FailedUnCordon: " + err.Error())
									}

									err = s.addLabelToNode(s.currentNode, NodeSkipLabel, "true")
									if err != nil {
										err = s.Stop("FailedAddLabelToNode: " + err.Error())
									}
								}

								err = s.selectNextNode()
								if err != nil {
									err = s.Stop("FailedSelectorNode: could not find a node that satisfies the condition")
								}
							}
						}
					}
				},
			},
		},
	)

	return
}

func (s *simulator) getPodsByNode(node *corev1.Node) ([]*corev1.Pod, error) {
	podList, err := s.GetPodsByNode(node.Name)
	if err != nil {
		return nil, err
	}

	klog.Infof("node %s has %d pods\n", node.Name, len(podList))
	return podList, nil

}

func initPod(input *corev1.Pod) *corev1.Pod {
	podSpec := input.Spec.DeepCopy()
	pod := &corev1.Pod{
		Spec: *podSpec,
	}
	apiv1.SetObjectDefaults_Pod(pod)

	// reset pod
	pod.Spec.NodeName = ""
	pod.Spec.SchedulerName = pkg.SchedulerName
	pod.Status = corev1.PodStatus{}

	// use simulated pod name with an index to construct the name
	pod.ObjectMeta.Name = input.Name
	pod.ObjectMeta.Namespace = input.Namespace
	pod.ObjectMeta.UID = types.UID(uuid.NewV4().String())
	// Add pod provisioner annotation
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}
	pod.ObjectMeta.Annotations[pkg.PodProvisioner] = pkg.SchedulerName

	return pod
}
