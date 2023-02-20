package clustercompression

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/k-cloud-labs/kluster-capacity/app/cmds/clustercompression/options"
)

const (
	NodeScaledDownFailedLabel = "kc.k-cloud-labs.io/node-scale-down-failed"
	KubernetesMasterNodeLabel = "node-role.kubernetes.io/master"
	NodeScaleDownDisableLabel = "kc.k-cloud-labs.io/scale-down-disabled"
)

type NodeFilter interface {
	SelectNode() *Status
}

func defaultFilterFunc() FilterFunc {
	return func(node *corev1.Node) *FilterStatus {
		if node.Labels != nil {
			_, ok := node.Labels[KubernetesMasterNodeLabel]
			if ok {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonMasterNode,
				}
			}

			_, ok = node.Labels[NodeScaledDownFailedLabel]
			if ok {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonFailedScaleDown,
				}
			}

			v, ok := node.Labels[NodeScaleDownDisableLabel]
			if ok && v == "true" {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonScaleDownDisabled,
				}
			}
		}
		return &FilterStatus{Success: true}
	}
}

type singleNodeFilter struct {
	clientset  clientset.Interface
	nodeFilter FilterFunc
}

type Status struct {
	Node      *corev1.Node
	ErrReason string
}

func NewNodeFilter(client clientset.Interface, getPodsByNode PodsByNodeFunc, excludeNodes []string, filterNodeOptions options.FilterNodeOptions) (NodeFilter, error) {
	excludeNodeMap := make(map[string]bool)
	for i := range excludeNodes {
		excludeNodeMap[excludeNodes[i]] = true
	}

	nodeFilter := NewOptions().
		WithFilter(defaultFilterFunc()).
		WithExcludeNodes(excludeNodeMap).
		WithExcludeTaintNodes(filterNodeOptions.ExcludeTaintNode).
		WithExcludeNotReadyNodes(filterNodeOptions.ExcludeNotReadyNode).
		WithIgnoreStaticPod(filterNodeOptions.IgnoreStaticPod).
		WithIgnoreCloneSet(filterNodeOptions.IgnoreCloneSet).
		WithIgnoreMirrorPod(filterNodeOptions.IgnoreMirrorPod).
		WithIgnoreVolumePod(filterNodeOptions.IgnoreVolumePod).
		WithPodsByNodeFunc(getPodsByNode).
		BuildFilterFunc()

	return &singleNodeFilter{
		clientset:  client,
		nodeFilter: nodeFilter,
	}, nil
}

func (g *singleNodeFilter) SelectNode() *Status {
	var (
		selected []*corev1.Node
		statuses []*FilterStatus
	)

	var nodeList []*corev1.Node
	nodes, err := g.clientset.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil
	}

	for i := range nodes.Items {
		nodeList = append(nodeList, &nodes.Items[i])
	}
	for _, v := range nodeList {
		status := g.nodeFilter(v)
		if status.Success {
			selected = append(selected, v)
		} else {
			statuses = append(statuses, status)
		}
	}

	if len(selected) == 0 {
		return convertFilterStatusesToStatus(statuses)
	}

	return &Status{Node: selected[0]}
}

func convertFilterStatusesToStatus(statuses []*FilterStatus) *Status {
	statusMap := make(map[string]int)

	for _, status := range statuses {
		statusMap[status.ErrReason]++
	}

	sb := strings.Builder{}
	for reason, count := range statusMap {
		_, _ = sb.WriteString(fmt.Sprintf("%d %s; ", count, reason))
	}

	return &Status{ErrReason: sb.String()}
}
