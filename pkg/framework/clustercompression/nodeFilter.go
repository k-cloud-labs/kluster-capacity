package clustercompression

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	NodeSkipperLabel = "kc.k-cloud-labs.io/node-skip"
)

type NodeFilter interface {
	SelectNode() *corev1.Node
}

type singleNodeFilter struct {
	clientset        clientset.Interface
	filter           FilterFunc
	skipTaintNode    bool
	skipNotReadyNode bool
	excludeNodes     []string
}

// FilterFunc is a filter for a node.
type FilterFunc func(*corev1.Node) bool

func NewNodeFilter(c clientset.Interface, skipTaintNode, skipNotReadyNode bool, excludeNodes []string) NodeFilter {
	return &singleNodeFilter{
		clientset:        c,
		skipTaintNode:    skipTaintNode,
		skipNotReadyNode: skipNotReadyNode,
		excludeNodes:     excludeNodes,
	}
}

// BuildFilterFunc builds a final FilterFunc based on Options.
func (g *singleNodeFilter) BuildFilterFunc() (FilterFunc, error) {
	return func(node *corev1.Node) bool {
		if g.filter != nil && !g.filter(node) {
			return false
		}
		if g.skipTaintNode && !taintNodeFilter(node) {
			return false
		}
		if g.skipNotReadyNode && notReadyNodeFilter(node) {
			return false
		}
		if HasNodeInExcludeNodes(g.excludeNodes, node.Name) {
			return false
		}
		_, ok := node.Labels[NodeSkipperLabel]
		if ok {
			return false
		}

		return true
	}, nil
}

func HasNodeInExcludeNodes(excludeNodes []string, nodeName string) bool {
	for i := range excludeNodes {
		if excludeNodes[i] == nodeName {
			return true
		}
	}

	return false
}

func (g *singleNodeFilter) SelectNode() *corev1.Node {
	res := []*corev1.Node{}

	var nodeList []*corev1.Node
	nodes, err := g.clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil
	}

	for i := range nodes.Items {
		nodeList = append(nodeList, &nodes.Items[i])
	}

	filterFuc, _ := g.BuildFilterFunc()
	for _, v := range nodeList {
		if filterFuc(v) {
			res = append(res, v)
		}
	}

	if len(res) == 0 {
		return nil
	}

	return res[0]
}

func taintNodeFilter(node *corev1.Node) bool {
	if node.Spec.Taints != nil {
		return false
	}
	return true
}

func notReadyNodeFilter(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		// We consider the node for scheduling only when its:
		// - NodeReady condition status is ConditionTrue,
		// - NodeNetworkUnavailable condition status is ConditionFalse.
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			return false
		}
	}
	return true
}
