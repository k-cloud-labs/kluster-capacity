package clustercompression

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

// FilterFunc is a filter for a node.
type FilterFunc func(*corev1.Node) bool

// WrapFilterFuncs wraps a set of FilterFunc in one.
func WrapFilterFuncs(filters ...FilterFunc) FilterFunc {
	return func(node *corev1.Node) bool {
		for _, filter := range filters {
			if filter != nil && !filter(node) {
				return false
			}
		}
		return true
	}
}

type Options struct {
	simulator           *simulator
	filter              FilterFunc
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

// WithoutNodes sets excluded node
func (o *Options) WithoutNodes(nodes map[string]bool) *Options {
	o.excludeNodes = nodes
	return o
}

// WithoutTaint set taint options
func (o *Options) WithoutTaint(excludeTaintNode bool) *Options {
	o.excludeTaintNode = excludeTaintNode
	return o
}

// WithoutNotReady set notReady options
func (o *Options) WithoutNotReady(excludeNotReadyNode bool) *Options {
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

func (o *Options) WithSimulator(s *simulator) *Options {
	o.simulator = s
	return o
}

// BuildFilterFunc builds a final FilterFunc based on Options.
func (o *Options) BuildFilterFunc() FilterFunc {
	return func(node *corev1.Node) bool {
		if o.filter != nil && !o.filter(node) {
			return false
		}

		if len(o.excludeNodes) > 0 && o.excludeNodes[node.Name] {
			return false
		}

		if o.excludeTaintNode && haveNodeTaint(node) {
			return false
		}
		if o.excludeNotReadyNode && isNodeReady(node) {
			return false
		}

		podList, err := o.simulator.GetPodsByNode(node.Name)
		if err != nil {
			return false
		}

		for i := range podList {
			if o.ignoreMirrorPod && utils.IsStaticPod(podList[i]) {
				return false
			}

			if o.ignoreMirrorPod && utils.IsMirrorPod(podList[i]) {
				return false
			}

			if o.ignoreVolumePod && utils.IsPodWithLocalStorage(podList[i]) {
				return false
			}

			if o.ignoreCloneSet && utils.IsCloneSetPod(podList[i].OwnerReferences) {
				return false
			}

		}
		return true
	}
}

func haveNodeTaint(node *corev1.Node) bool {
	return len(node.Spec.Taints) != 0
}

func isNodeReady(node *corev1.Node) bool {
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
