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
	NodeScaledDownFailedLabel  = "kc.k-cloud-labs.io/node-scale-down-failed"
	NodeScaledDownSuccessLabel = "kc.k-cloud-labs.io/node-scale-down-success"
	KubernetesMasterNodeLabel  = "node-role.kubernetes.io/master"
	NodeScaleDownDisableLabel  = "kc.k-cloud-labs.io/scale-down-disabled"
)

type NodeFilter interface {
	SelectNode() *Status
	Done()
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

			_, ok = node.Labels[NodeScaledDownSuccessLabel]
			if ok {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonSuccessScaleDown,
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
	clientset      clientset.Interface
	nodeFilter     FilterFunc
	selectedCount  int
	candidateNode  []*corev1.Node
	candidateIndex int
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
	if len(g.candidateNode) != 0 && g.candidateIndex <= len(g.candidateNode)-1 {
		selectNode := g.candidateNode[g.candidateIndex]
		g.candidateIndex++
		if g.candidateIndex == len(g.candidateNode) {
			g.candidateNode = nil
			g.candidateIndex = 0
		}
		return &Status{Node: selectNode}
	}

	g.candidateNode = nil
	g.candidateIndex = 0

	var statuses []*FilterStatus
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
			g.candidateNode = append(g.candidateNode, v)
		} else {
			statuses = append(statuses, status)
		}
	}

	if len(g.candidateNode) == 0 {
		return convertFilterStatusesToStatus(statuses, g.selectedCount)
	}

	g.candidateIndex++

	return &Status{Node: g.candidateNode[0]}
}

func (g *singleNodeFilter) Done() {
	g.selectedCount++
}

func convertFilterStatusesToStatus(statuses []*FilterStatus, selectedCount int) *Status {
	statusMap := make(map[string]int)

	for _, status := range statuses {
		statusMap[status.ErrReason]++
	}

	// for taint added by self
	if count, ok := statusMap[ErrReasonTaintNode]; ok {
		realCount := count - selectedCount
		if realCount == 0 {
			delete(statusMap, ErrReasonTaintNode)
		} else {
			statusMap[ErrReasonTaintNode] = realCount
		}
	}

	sb := strings.Builder{}
	for reason, count := range statusMap {
		_, _ = sb.WriteString(fmt.Sprintf("%d %s; ", count, reason))
	}

	return &Status{ErrReason: sb.String()}
}
