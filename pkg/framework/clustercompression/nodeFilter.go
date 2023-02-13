package clustercompression

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
)

const (
	NodeSkipLabel             = "kc.k-cloud-labs.io/node-skip"
	KubernetesMasterNodeLabel = "node-role.kubernetes.io/master"
	NodeScaleDownDisableLabel = "kc.k-cloud-labs.io/scale-down-disabled"
)

type NodeFilter interface {
	SelectNode() *corev1.Node
}

func defaultFilterFunc() FilterFunc {
	return func(node *corev1.Node) bool {
		if node.Labels != nil {
			_, ok := node.Labels[NodeSkipLabel]
			if ok {
				return false
			}

			_, ok = node.Labels[KubernetesMasterNodeLabel]
			if ok {
				return false
			}

			v, ok := node.Labels[NodeScaleDownDisableLabel]
			if ok && v == "true" {
				return false
			}
		}
		return true
	}
}

type singleNodeFilter struct {
	clientset  clientset.Interface
	nodeFilter FilterFunc
}

func NewNodeFilter(s *simulator, excludeNodes []string, filterNodeOptions options.FilterNodeOptions) (NodeFilter, error) {
	excludeNodeMap := make(map[string]bool)
	for i := range excludeNodes {
		excludeNodeMap[excludeNodes[i]] = true
	}

	nodeFilter := NewOptions().
		WithFilter(defaultFilterFunc()).
		WithoutNodes(excludeNodeMap).
		WithoutTaint(filterNodeOptions.ExcludeTaintNode).
		WithoutNotReady(filterNodeOptions.ExcludeNotReadyNode).
		WithIgnoreStaticPod(filterNodeOptions.IgnoreStaticPod).
		WithIgnoreCloneSet(filterNodeOptions.IgnoreCloneSet).
		WithIgnoreMirrorPod(filterNodeOptions.IgnoreMirrorPod).
		WithIgnoreVolumePod(filterNodeOptions.IgnoreVolumePod).
		WithSimulator(s).
		BuildFilterFunc()

	return &singleNodeFilter{
		clientset:  s.fakeClient,
		nodeFilter: nodeFilter,
	}, nil
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
	for _, v := range nodeList {
		if g.nodeFilter(v) {
			res = append(res, v)
		}
	}

	if len(res) == 0 {
		return nil
	}

	return res[0]
}
