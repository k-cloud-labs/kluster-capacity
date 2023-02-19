package capacityestimation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type CapacityEstimationReview struct {
	metav1.TypeMeta
	Spec   CapacityEstimationReviewSpec   `json:"spec"`
	Status CapacityEstimationReviewStatus `json:"status"`
}

type CapacityEstimationReviews []*CapacityEstimationReview

type CapacityEstimationReviewSpec struct {
	// the pod desired for scheduling
	Templates       []corev1.Pod    `json:"templates"`
	PodRequirements []*Requirements `json:"podRequirements"`
}

type CapacityEstimationReviewStatus struct {
	CreationTimestamp time.Time `json:"creationTimestamp"`
	// actual number of replicas that could schedule
	Replicas   int32                                       `json:"replicas"`
	StopReason *CapacityEstimationReviewScheduleStopReason `json:"stopReason"`
	// per node information about the scheduling simulation
	Pods []*CapacityEstimationReviewResult `json:"pods"`
}

type CapacityEstimationReviewResult struct {
	PodName string `json:"podName"`
	// numbers of replicas on nodes
	ReplicasOnNodes []*ReplicasOnNode `json:"replicasOnNodes"`
	// reason why no more pods could schedule (if any on this node)
	Summary []StopReasonSummary `json:"summary"`
}

type ReplicasOnNode struct {
	NodeName string `json:"nodeName"`
	Replicas int    `json:"replicas"`
}

type StopReasonSummary struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type Resources struct {
	PrimaryResources corev1.ResourceList           `json:"primaryResources"`
	ScalarResources  map[corev1.ResourceName]int64 `json:"scalarResources"`
}

type Requirements struct {
	PodName       string              `json:"podName"`
	Resources     *framework.Resource `json:"resources"`
	NodeSelectors map[string]string   `json:"nodeSelectors"`
}

type CapacityEstimationReviewScheduleStopReason struct {
	StopType    string `json:"stopType"`
	StopMessage string `json:"stopMessage"`
}

func (r *CapacityEstimationReview) Print(verbose bool, format string) error {
	switch format {
	case "json":
		return utils.PrintJson(r)
	case "yaml":
		return utils.PrintYaml(r)
	case "":
		capacityEstimationReviewPrettyPrint(r, verbose)
		return nil
	default:
		return fmt.Errorf("output format %q not recognized", format)
	}
}

func (r CapacityEstimationReviews) Print(verbose bool, format string) error {
	t := table.NewWriter()
	t.AppendHeader(table.Row{"spec", "replicas"})
	for i, review := range r {
		if i > 0 && (format != "" || verbose) {
			fmt.Println("---------------------------------------------------------------")
		}
		switch format {
		case "json":
			err := utils.PrintJson(review)
			if err != nil {
				return err
			}
		case "yaml":
			err := utils.PrintYaml(review)
			if err != nil {
				return err
			}
		case "":
			if verbose {
				capacityEstimationReviewPrettyPrint(review, verbose)
			} else {
				output, err := json.Marshal(review.Spec.PodRequirements[0])
				if err != nil {
					return err
				}
				t.AppendRow(table.Row{string(output), review.Status.Replicas})
			}
		default:
			return fmt.Errorf("output format %q not recognized", format)
		}
	}

	if format == "" && !verbose {
		fmt.Println(t.Render())
	}

	return nil
}

func generateReport(pods []*corev1.Pod, status pkg.Status) *CapacityEstimationReview {
	return &CapacityEstimationReview{
		Spec:   getReviewSpec(pods),
		Status: getReviewStatus(pods, status),
	}
}

func getMainStopReason(message string) *CapacityEstimationReviewScheduleStopReason {
	slicedMessage := strings.Split(message, "\n")
	colon := strings.Index(slicedMessage[0], ":")

	reason := &CapacityEstimationReviewScheduleStopReason{
		StopType:    slicedMessage[0][:colon],
		StopMessage: strings.Trim(slicedMessage[0][colon+1:], " "),
	}
	return reason
}

func parsePodsReview(templatePods []*corev1.Pod, status pkg.Status) []*CapacityEstimationReviewResult {
	templatesCount := len(templatePods)
	result := make([]*CapacityEstimationReviewResult, 0)

	for i := 0; i < templatesCount; i++ {
		result = append(result, &CapacityEstimationReviewResult{
			ReplicasOnNodes: make([]*ReplicasOnNode, 0),
			PodName:         templatePods[i].Name,
		})
	}

	for i, pod := range status.Pods {
		nodeName := pod.Spec.NodeName
		first := true
		for _, sum := range result[i%templatesCount].ReplicasOnNodes {
			if sum.NodeName == nodeName {
				sum.Replicas++
				first = false
			}
		}
		if first {
			result[i%templatesCount].ReplicasOnNodes = append(result[i%templatesCount].ReplicasOnNodes, &ReplicasOnNode{
				NodeName: nodeName,
				Replicas: 1,
			})
		}
	}

	slicedMessage := strings.Split(status.StopReason, "\n")
	if len(slicedMessage) == 1 {
		return result
	}

	return result
}

func getReviewSpec(podTemplates []*corev1.Pod) CapacityEstimationReviewSpec {
	podCopies := make([]corev1.Pod, len(podTemplates))
	deepCopyPods(podTemplates, podCopies)
	return CapacityEstimationReviewSpec{
		Templates:       podCopies,
		PodRequirements: getPodsRequirements(podTemplates),
	}
}

func getReviewStatus(pods []*corev1.Pod, status pkg.Status) CapacityEstimationReviewStatus {
	return CapacityEstimationReviewStatus{
		CreationTimestamp: time.Now(),
		Replicas:          int32(len(status.Pods)),
		StopReason:        getMainStopReason(status.StopReason),
		Pods:              parsePodsReview(pods, status),
	}
}

func deepCopyPods(in []*corev1.Pod, out []corev1.Pod) {
	for i, pod := range in {
		out[i] = *pod.DeepCopy()
	}
}

func getPodsRequirements(pods []*corev1.Pod) []*Requirements {
	result := make([]*Requirements, 0)
	for _, pod := range pods {
		podRequirements := &Requirements{
			PodName:       pod.Name,
			Resources:     utils.ComputePodResourceRequest(pod),
			NodeSelectors: pod.Spec.NodeSelector,
		}
		result = append(result, podRequirements)
	}
	return result
}

func instancesSum(replicasOnNodes []*ReplicasOnNode) int {
	result := 0
	for _, v := range replicasOnNodes {
		result += v.Replicas
	}
	return result
}

func capacityEstimationReviewPrettyPrint(r *CapacityEstimationReview, verbose bool) {
	if verbose {
		for _, req := range r.Spec.PodRequirements {
			fmt.Printf("%v pod requirements:\n", req.PodName)
			fmt.Printf("\t- CPU(m): %v\n", req.Resources.MilliCPU)
			fmt.Printf("\t- Memory(B): %v\n", req.Resources.Memory)
			if req.Resources.ScalarResources != nil {
				fmt.Printf("\t- ScalarResources: %v\n", req.Resources.ScalarResources)
			}

			if req.NodeSelectors != nil {
				fmt.Printf("\t- NodeSelector: %v\n", labels.SelectorFromSet(req.NodeSelectors).String())
			}
			fmt.Printf("\n")
		}
	}

	for _, pod := range r.Status.Pods {
		if verbose {
			fmt.Printf("The cluster can schedule %v instance(s) of the pod %v.\n", instancesSum(pod.ReplicasOnNodes), pod.PodName)
		} else {
			fmt.Printf("%v\n", instancesSum(pod.ReplicasOnNodes))
		}
	}

	if verbose {
		fmt.Printf("\nTermination reason: %v: %v\n", r.Status.StopReason.StopType, r.Status.StopReason.StopMessage)
	}

	if verbose && r.Status.Replicas > 0 {
		for _, pod := range r.Status.Pods {
			if pod.Summary != nil {
				fmt.Printf("fit failure summary on nodes: ")
				for _, fs := range pod.Summary {
					fmt.Printf("%v (%v), ", fs.Reason, fs.Count)
				}
				fmt.Printf("\n")
			}
		}
		fmt.Printf("\nPod distribution among nodes:\n")
		for _, pod := range r.Status.Pods {
			fmt.Printf("%v\n", pod.PodName)
			for _, ron := range pod.ReplicasOnNodes {
				fmt.Printf("\t- %v: %v instance(s)\n", ron.NodeName, ron.Replicas)
			}
		}
	}
}
