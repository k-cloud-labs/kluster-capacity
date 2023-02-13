package clustercompression

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type ClusterCompressionReview struct {
	metav1.TypeMeta
	Status ClusterCompressionReviewReviewStatus `json:"status"`
}

type ClusterCompressionReviewReviewStatus struct {
	CreationTimestamp  time.Time                                   `json:"creationTimestamp"`
	Replicas           int32                                       `json:"replicas"`
	StopReason         *ClusterCompressionReviewScheduleFailReason `json:"stopReason"`
	ScaleDownNodeNames []string                                    `json:"scaleDownNodeNames"`
}

type ClusterCompressionReviewScheduleFailReason struct {
	FailType    string `json:"failType"`
	FailMessage string `json:"failMessage"`
}

func generateReport(status pkgframework.Status) *ClusterCompressionReview {
	return &ClusterCompressionReview{
		Status: getReviewStatus(status),
	}
}

func getReviewStatus(status pkgframework.Status) ClusterCompressionReviewReviewStatus {
	return ClusterCompressionReviewReviewStatus{
		CreationTimestamp:  time.Now(),
		Replicas:           int32(len(status.ScaleDownNodeNames)),
		StopReason:         getMainStopReason(status.StopReason),
		ScaleDownNodeNames: status.ScaleDownNodeNames,
	}
}

func getMainStopReason(message string) *ClusterCompressionReviewScheduleFailReason {
	slicedMessage := strings.Split(message, "\n")
	colon := strings.Index(slicedMessage[0], ":")

	reason := &ClusterCompressionReviewScheduleFailReason{
		FailType:    slicedMessage[0][:colon],
		FailMessage: strings.Trim(slicedMessage[0][colon+1:], " "),
	}
	return reason
}

func (r *ClusterCompressionReview) Print(verbose bool, format string) error {
	switch format {
	case "json":
		return utils.PrintJson(r)
	default:
		return clusterCapacityReviewDefaultPrint(r)
	}
}

func clusterCapacityReviewDefaultPrint(r *ClusterCompressionReview) error {
	if r != nil && r.Status.ScaleDownNodeNames != nil && len(r.Status.ScaleDownNodeNames) > 0 {
		for i := range r.Status.ScaleDownNodeNames {
			fmt.Println(r.Status.ScaleDownNodeNames[i])
		}
	} else {
		fmt.Println("There are no nodes in the cluster to scale down")
	}

	return nil
}
