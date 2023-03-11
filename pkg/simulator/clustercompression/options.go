package clustercompression

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

const (
	ErrReasonFailedScaleDown   = "node(s) can't be scale down because of insufficient resource in other nodes"
	ErrReasonScaleDownDisabled = "node(s) have label with scale down disabled"
	ErrReasonMasterNode        = "master node(s)"
	ErrReasonTaintNode         = "node(s) have taint"
	ErrReasonExcludeNode       = "exclude node(s)"
	ErrReasonNotReadyNode      = "not ready node(s)"
	ErrReasonStaticPod         = "node(s) have static pod"
	ErrReasonMirrorPod         = "node(s) have mirror pod"
	ErrReasonCloneset          = "node(s) have inplace update pod"
	ErrReasonVolumePod         = "node(s) have pod used hostpath"
	ErrReasonUnknown           = "node(s) have unknown error"
)

// FilterFunc is a filter for a node.
type FilterFunc func(*corev1.Node) *FilterStatus
type PodsByNodeFunc func(name string) ([]*corev1.Pod, error)

type FilterStatus struct {
	// true means that node is able to simulate
	Success   bool
	ErrReason string
}

type Options struct {
	filter              FilterFunc
	getPodsByNode       PodsByNodeFunc
	excludeNodes        map[string]bool
	excludeTaintNode    bool
	excludeNotReadyNode bool
	ignoreStaticPod     bool
	ignoreMirrorPod     bool
	ignoreCloneSet      bool
	ignoreVolumePod     bool
}

// NewOptions returns an empty Options.
func NewOptions() *Options {
	return &Options{}
}

// WithFilter sets a node filter.
func (o *Options) WithFilter(filter FilterFunc) *Options {
	o.filter = filter
	return o
}

// WithExcludeNodes sets excluded node
func (o *Options) WithExcludeNodes(nodes map[string]bool) *Options {
	o.excludeNodes = nodes
	return o
}

// WithExcludeTaintNodes set taint options
func (o *Options) WithExcludeTaintNodes(excludeTaintNode bool) *Options {
	o.excludeTaintNode = excludeTaintNode
	return o
}

// WithExcludeNotReadyNodes set notReady options
func (o *Options) WithExcludeNotReadyNodes(excludeNotReadyNode bool) *Options {
	o.excludeNotReadyNode = excludeNotReadyNode
	return o
}

// WithIgnoreStaticPod set ignoreStaticPod options
func (o *Options) WithIgnoreStaticPod(ignoreStaticPod bool) *Options {
	o.ignoreStaticPod = ignoreStaticPod
	return o
}

// WithIgnoreMirrorPod set ignoreMirrorPod options
func (o *Options) WithIgnoreMirrorPod(ignoreMirrorPod bool) *Options {
	o.ignoreMirrorPod = ignoreMirrorPod
	return o
}

// WithIgnoreCloneSet set ignoreCloneSet options
func (o *Options) WithIgnoreCloneSet(ignoreCloneSet bool) *Options {
	o.ignoreCloneSet = ignoreCloneSet
	return o
}

// WithIgnoreVolumePod set ignoreVolumePod options
func (o *Options) WithIgnoreVolumePod(ignoreVolumePod bool) *Options {
	o.ignoreVolumePod = ignoreVolumePod
	return o
}

func (o *Options) WithPodsByNodeFunc(podsByNodeFunc PodsByNodeFunc) *Options {
	o.getPodsByNode = podsByNodeFunc
	return o
}

// BuildFilterFunc builds a final FilterFunc based on Options.
func (o *Options) BuildFilterFunc() FilterFunc {
	return func(node *corev1.Node) *FilterStatus {
		if o.filter != nil {
			status := o.filter(node)
			if status != nil && !status.Success {
				return status
			}
		}

		if len(o.excludeNodes) > 0 && o.excludeNodes[node.Name] {
			return &FilterStatus{
				Success:   false,
				ErrReason: ErrReasonExcludeNode,
			}
		}

		if o.excludeTaintNode && haveNodeTaint(node) {
			return &FilterStatus{
				Success:   false,
				ErrReason: ErrReasonTaintNode,
			}
		}
		if o.excludeNotReadyNode && isNodeNotReady(node) {
			return &FilterStatus{
				Success:   false,
				ErrReason: ErrReasonNotReadyNode,
			}
		}

		podList, err := o.getPodsByNode(node.Name)
		if err != nil {
			return &FilterStatus{
				Success:   false,
				ErrReason: ErrReasonUnknown,
			}
		}

		for i := range podList {
			if o.ignoreStaticPod && utils.IsStaticPod(podList[i]) {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonStaticPod,
				}
			}

			if o.ignoreMirrorPod && utils.IsMirrorPod(podList[i]) {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonMirrorPod,
				}
			}

			if o.ignoreVolumePod && utils.IsPodWithLocalStorage(podList[i]) {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonVolumePod,
				}
			}

			if o.ignoreCloneSet && utils.IsCloneSetPod(podList[i].OwnerReferences) {
				return &FilterStatus{
					Success:   false,
					ErrReason: ErrReasonCloneset,
				}
			}

		}
		return &FilterStatus{Success: true}
	}
}

func haveNodeTaint(node *corev1.Node) bool {
	return len(node.Spec.Taints) != 0
}

func isNodeNotReady(node *corev1.Node) bool {
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
