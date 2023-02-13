package clustercompression

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ClusterCompressionReview struct {
	metav1.TypeMeta
	Status ClusterCompressionReviewReviewStatus `json:"status"`
}

type ClusterCompressionReviewReviewStatus struct {
	CreationTimestamp time.Time                                   `json:"creationTimestamp"`
	Replicas          int32                                       `json:"replicas"`
	StopReason        *ClusterCompressionReviewScheduleFailReason `json:"stopReason"`
	NodeNames         []string                                    `json:"nodeNames"`
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
		CreationTimestamp: time.Now(),
		Replicas:          int32(len(status.NodeNameList)),
		StopReason:        getMainStopReason(status.StopReason),
		NodeNames:         status.NodeNameList,
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
	return clusterCapacityReviewPrintJson(r)
}

func clusterCapacityReviewPrintJson(r *ClusterCompressionReview) error {
	jsoned, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("Failed to create json: %v", err)
	}
	fmt.Println("clusterCompression running result:", string(jsoned))
	return nil
}
