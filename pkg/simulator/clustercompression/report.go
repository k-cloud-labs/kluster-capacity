package clustercompression

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type ClusterCompressionReview struct {
	metav1.TypeMeta
	Status ClusterCompressionReviewReviewStatus `json:"status"`
}

type ClusterCompressionReviewReviewStatus struct {
	CreationTimestamp  time.Time                                   `json:"creationTimestamp"`
	StopReason         *ClusterCompressionReviewScheduleStopReason `json:"stopReason"`
	ScaleDownNodeNames []string                                    `json:"scaleDownNodeNames"`
}

type ClusterCompressionReviewScheduleStopReason struct {
	StopType    string `json:"stopType"`
	StopMessage string `json:"stopMessage"`
}

func generateReport(status pkg.Status) *ClusterCompressionReview {
	return &ClusterCompressionReview{
		Status: getReviewStatus(status),
	}
}

func getReviewStatus(status pkg.Status) ClusterCompressionReviewReviewStatus {
	return ClusterCompressionReviewReviewStatus{
		CreationTimestamp:  time.Now(),
		StopReason:         getMainStopReason(status.StopReason),
		ScaleDownNodeNames: status.NodesToScaleDown,
	}
}

func getMainStopReason(message string) *ClusterCompressionReviewScheduleStopReason {
	slicedMessage := strings.Split(message, "\n")
	colon := strings.Index(slicedMessage[0], ":")

	reason := &ClusterCompressionReviewScheduleStopReason{
		StopType:    slicedMessage[0][:colon],
		StopMessage: strings.Trim(slicedMessage[0][colon+1:], " "),
	}
	return reason
}

func (r *ClusterCompressionReview) Print(verbose bool, format string) error {
	switch format {
	case "json":
		return utils.PrintJson(r)
	default:
		return clusterCapacityReviewDefaultPrint(r, verbose)
	}
}

func clusterCapacityReviewDefaultPrint(r *ClusterCompressionReview, verbose bool) error {
	if r != nil && len(r.Status.ScaleDownNodeNames) > 0 {
		if verbose {
			fmt.Printf("%d node(s) in the cluster can be scaled down.\n", len(r.Status.ScaleDownNodeNames))
			fmt.Printf("\nTermination reason: %v: %v\n", r.Status.StopReason.StopType, r.Status.StopReason.StopMessage)
			fmt.Printf("\nnodes selected to be scaled down:\n")

			for i := range r.Status.ScaleDownNodeNames {
				fmt.Printf("\t- %s\n", r.Status.ScaleDownNodeNames[i])
			}
		} else {
			for i := range r.Status.ScaleDownNodeNames {
				fmt.Println(r.Status.ScaleDownNodeNames[i])
			}
		}
	} else {
		fmt.Println("No nodes in the cluster can be scaled down.")
		fmt.Printf("\nTermination reason: %v: %v\n", r.Status.StopReason.StopType, r.Status.StopReason.StopMessage)
	}

	return nil
}
