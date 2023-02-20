package schedulersimulation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type SchedulerSimulationReview struct {
	UnschedulablePods []*corev1.Pod    `json:"unschedulablePods"`
	Details           []ScheduleDetail `json:"details"`
	StopReason        string           `json:"stopReason"`
}

type ScheduleDetail struct {
	NodeName        string              `json:"nodeName"`
	Replicas        int                 `json:"replicas"`
	NodeAllocatable corev1.ResourceList `json:"nodeAllocatable"`
	PodRequest      framework.Resource  `json:"podRequest"`
}

func (r *SchedulerSimulationReview) Print(verbose bool, format string) error {
	switch format {
	case "json":
		return utils.PrintJson(r)
	case "yaml":
		return utils.PrintYaml(r)
	case "":
		prettyPrint(r, verbose)
		return nil
	default:
		return fmt.Errorf("output format %q not recognized", format)
	}
}

func prettyPrint(r *SchedulerSimulationReview, verbose bool) {
	fmt.Printf("Termination reason: %s\n\n", r.StopReason)
	if len(r.UnschedulablePods) > 0 {
		fmt.Printf("Unschedulabel pods(%d):\n", len(r.UnschedulablePods))
	}

	for _, pod := range r.UnschedulablePods {
		if verbose {
			fmt.Printf("- %v/%s, reason: %s, request: %+v\n", pod.Namespace, pod.Name, getUnschedulableReason(pod), *utils.ComputePodResourceRequest(pod))
		} else {
			fmt.Printf("- %v/%s, reason: %s\n", pod.Namespace, pod.Name, getUnschedulableReason(pod))
		}
	}

	if len(r.UnschedulablePods) > 0 {
		fmt.Printf("\n\n")
	}
	fmt.Printf("Pod distribution among nodes:\n")

	for _, detail := range r.Details {
		if verbose {
			fmt.Printf("\t- %v: %v instance(s), allocatable: %v, requested: %v\n", detail.NodeName, detail.Replicas, detail.NodeAllocatable, detail.PodRequest)
		} else {
			fmt.Printf("\t- %v: %v instance(s)\n", detail.NodeName, detail.Replicas)
		}
	}
}

func getUnschedulableReason(pod *corev1.Pod) string {
	for _, podCondition := range pod.Status.Conditions {
		// Only for pending pods provisioned by ce
		if podCondition.Type == corev1.PodScheduled && podCondition.Status == corev1.ConditionFalse &&
			podCondition.Reason == corev1.PodReasonUnschedulable {
			return podCondition.Message
		}
	}

	return ""
}

func generateReport(status pkg.Status) *SchedulerSimulationReview {
	details := make([]ScheduleDetail, 0)
	unschedulablePods := make([]*corev1.Pod, 0)
	nodePodMap := make(map[string][]*corev1.Pod)

	for _, pod := range status.Pods {
		nodePodMap[pod.Spec.NodeName] = append(nodePodMap[pod.Spec.NodeName], pod)
	}

	for node, pods := range nodePodMap {
		if node == "" {
			unschedulablePods = append(unschedulablePods, pods...)
			continue
		}

		var request framework.Resource

		for _, pod := range pods {
			addResource(&request, utils.ComputePodResourceRequest(pod))
		}

		detail := ScheduleDetail{
			NodeName:   node,
			Replicas:   len(nodePodMap[node]),
			PodRequest: request,
		}
		if node, ok := status.Nodes[node]; ok {
			detail.NodeAllocatable = node.Status.Allocatable
		}
		details = append(details, detail)
	}

	return &SchedulerSimulationReview{
		UnschedulablePods: unschedulablePods,
		Details:           details,
		StopReason:        status.StopReason,
	}
}

func addResource(source *framework.Resource, res *framework.Resource) {
	source.MilliCPU += res.MilliCPU
	source.Memory += res.Memory
	source.EphemeralStorage += res.EphemeralStorage
	if source.ScalarResources == nil && len(res.ScalarResources) > 0 {
		source.ScalarResources = map[corev1.ResourceName]int64{}
	}
	for rName, rQuant := range res.ScalarResources {
		source.ScalarResources[rName] += rQuant
	}
}
