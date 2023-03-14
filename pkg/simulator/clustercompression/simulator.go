package clustercompression

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
	"github.com/k-cloud-labs/kluster-capacity/pkg"
	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

const FailedSelectNode = "FailedSelectNode: could not find a node that satisfies the condition"

// only support one scheduler for now and the scheduler name is "default-scheduler"
type simulator struct {
	pkg.Framework

	maxSimulated             int
	simulated                int
	fakeClient               clientset.Interface
	createdPods              []*corev1.Pod
	createPodIndex           int
	currentNode              string
	currentNodeUnschedulable bool
	bindSuccessPodCount      int
	nodeFilter               NodeFilter
}

// NewCCSimulatorExecutor create a ce simulator which is completely independent of apiserver so no need
// for kubeconfig nor for apiserver url
func NewCCSimulatorExecutor(conf *options.ClusterCompressionConfig) (pkg.Simulator, error) {
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
		createPodIndex:      0,
		maxSimulated:        conf.Options.MaxLimit,
	}

	// add your custom event handlers
	err = s.addEventHandlers(cc.InformerFactory)
	if err != nil {
		return nil, err
	}

	framework, err := pkgframework.NewKubeSchedulerFramework(cc, kubeConfig,
		pkgframework.WithExcludeNodes(conf.Options.ExcludeNodes),
		pkgframework.WithPostBindHook(s.postBindHook),
	)
	if err != nil {
		return nil, err
	}

	s.Framework = framework
	s.fakeClient = cc.Client
	nodeFilter, err := NewNodeFilter(s.fakeClient, s.GetPodsByNode, conf.Options.ExcludeNodes, conf.Options.FilterNodeOptions)
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
	err = s.selectNextNode()
	if err != nil {
		return s.Stop(fmt.Sprintf("%s, %s", FailedSelectNode, err.Error()))
	}

	return nil
}

func (s *simulator) Report() pkg.Printer {
	klog.V(2).Infof("the following nodes can be offline to save resources: %v", s.Status().NodesToScaleDown)
	klog.V(2).Infof("the clusterCompression StopReason: %s", s.Status().StopReason)
	return generateReport(s.Status())
}

func (s *simulator) postBindHook(bindPod *corev1.Pod) error {

	if s.maxSimulated > 0 && s.simulated >= s.maxSimulated {
		return s.Stop(fmt.Sprintf("LimitReached: maximum number of nodes simulated: %v", s.maxSimulated))
	}

	s.bindSuccessPodCount++
	if len(s.createdPods) > 0 && s.createPodIndex < len(s.createdPods) {
		klog.V(2).Infof("create %d pod: %s", s.createPodIndex, s.createdPods[s.createPodIndex].Namespace+"/"+s.createdPods[s.createPodIndex].Name)
		_, err := s.fakeClient.CoreV1().Pods(s.createdPods[s.createPodIndex].Namespace).Create(context.TODO(), utils.InitPod(s.createdPods[s.createPodIndex]), metav1.CreateOptions{})
		if err != nil {
			return err
		}
		s.createPodIndex++
	} else if s.bindSuccessPodCount == len(s.createdPods) {
		klog.V(2).Infof("add node %s to simulator status", s.currentNode)
		s.UpdateNodesToScaleDown(s.currentNode)

		err := s.addLabelToNode(s.currentNode, NodeScaledDownSuccessLabel, "true")
		if err != nil {
			_ = s.Stop("FailedAddLabelToNode: " + err.Error())
		}

		s.simulated++
		s.nodeFilter.Done()

		err = s.selectNextNode()
		if err != nil {
			return s.Stop(fmt.Sprintf("%s, %s", FailedSelectNode, err.Error()))
		}
	}

	return nil
}

func (s *simulator) selectNextNode() error {
	s.Status().SelectNodeCountInc()
	status := s.nodeFilter.SelectNode()
	if status != nil && status.Node == nil {
		return errors.New(status.ErrReason)
	}
	node := status.Node
	klog.V(2).Infof("select node %s to simulate\n", node.Name)

	s.createdPods = nil
	s.bindSuccessPodCount = 0
	s.createPodIndex = 0
	s.currentNode = node.Name
	s.currentNodeUnschedulable = node.Spec.Unschedulable

	err := s.cordon(node)
	if err != nil {
		return err
	}

	err = s.deletePodsByNode(node)
	if err != nil {
		return err
	}
	klog.V(2).Infof("node %s needs to create %d pods\n", node.Name, len(s.createdPods))

	if len(s.createdPods) > 0 {
		_, err = s.fakeClient.CoreV1().Pods(s.createdPods[s.createPodIndex].Namespace).Create(context.TODO(), utils.InitPod(s.createdPods[s.createPodIndex]), metav1.CreateOptions{})
		klog.V(2).Infof("create %d pod: %s", s.createPodIndex, s.createdPods[s.createPodIndex].Namespace+"/"+s.createdPods[s.createPodIndex].Name)
		if err != nil {
			return err
		}
		s.createPodIndex++
	} else {
		klog.V(2).Infof("add node %s to simulator status", s.currentNode)
		s.UpdateNodesToScaleDown(s.currentNode)

		err := s.addLabelToNode(s.currentNode, NodeScaledDownSuccessLabel, "true")
		if err != nil {
			_ = s.Stop("FailedAddLabelToNode: " + err.Error())
		}

		s.simulated++
		s.nodeFilter.Done()
		return s.selectNextNode()
	}

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
		Key:    corev1.TaintNodeUnschedulable,
		Effect: corev1.TaintEffectNoSchedule,
	}
	taints = append(taints, unScheduleTaint)

	for i := range copy.Spec.Taints {
		if copy.Spec.Taints[i].Key != corev1.TaintNodeUnschedulable {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.V(2).Infof("cordon node %s successfully\n", node.Name)
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
		if copy.Spec.Taints[i].Key != corev1.TaintNodeUnschedulable {
			taints = append(taints, copy.Spec.Taints[i])
		}
	}
	copy.Spec.Taints = taints

	_, err = s.fakeClient.CoreV1().Nodes().Update(context.TODO(), copy, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	klog.V(2).Infof("unCordon node %s successfully\n", nodeName)
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
	klog.V(2).Infof("add node %s failed scale down label successfully\n", node.Name)
	return nil
}

func (s *simulator) updatePodsFromCreatedPods() error {
	podList := s.createdPods
	for index := s.createPodIndex - 1; index >= 0; index-- {
		err := s.fakeClient.CoreV1().Pods(podList[index].Namespace).Delete(context.TODO(), podList[index].Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		klog.V(2).Infof("delete %d pod: %s", index, s.createdPods[index].Namespace+"/"+s.createdPods[index].Name)
	}

	for index := range s.createdPods {
		_, err := s.fakeClient.CoreV1().Pods(s.createdPods[index].Namespace).Create(context.TODO(), s.createdPods[index], metav1.CreateOptions{})
		if err != nil {
			return err
		}
		klog.V(2).Infof("create %d pod: %s", index, s.createdPods[index].Namespace+"/"+s.createdPods[index].Name)
	}

	return nil
}

func (s *simulator) deletePodsByNode(node *corev1.Node) error {
	podList, err := s.getPodsByNode(node)
	if err != nil {
		return err
	}

	var createdPods []*corev1.Pod
	for i := range podList {
		if !utils.IsDaemonsetPod(podList[i].OwnerReferences) && podList[i].DeletionTimestamp == nil {
			createdPods = append(createdPods, podList[i])
			err := s.fakeClient.CoreV1().Pods(podList[i].Namespace).Delete(context.TODO(), podList[i].Name, metav1.DeleteOptions{})
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
								s.Status().FailedSchedulerCountInc()
								// 1. Empty all Pods created by fake before
								// 2. Uncordon this node if needed
								// 3. Type the flags that cannot be filtered, clear the flags that prohibit scheduling, add failed scale down label, then selectNextNode
								klog.V(2).Infof("Failed scheduling pod %s, reason: %s, message: %s\n", pod.Namespace+"/"+pod.Name, podCondition.Reason, podCondition.Message)
								err = s.updatePodsFromCreatedPods()
								if err != nil {
									err = s.Stop("FailedDeletePodsFromCreatedPods: " + err.Error())
								}

								if !s.currentNodeUnschedulable {
									err = s.unCordon(s.currentNode)
									if err != nil {
										err = s.Stop("FailedUnCordon: " + err.Error())
									}
								}

								err = s.addLabelToNode(s.currentNode, NodeScaledDownFailedLabel, "true")
								if err != nil {
									err = s.Stop("FailedAddLabelToNode: " + err.Error())
								}

								err = s.selectNextNode()
								if err != nil {
									_ = s.Stop(fmt.Sprintf("%s, %s", FailedSelectNode, err.Error()))
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

	klog.V(2).Infof("node %s has %d pods\n", node.Name, len(podList))
	return podList, nil
}
