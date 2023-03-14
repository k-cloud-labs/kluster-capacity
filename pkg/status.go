package pkg

import (
	corev1 "k8s.io/api/core/v1"
)

// Status capture all scheduled pods with reason why the estimation could not continue
type Status struct {
	// all pods
	Pods []corev1.Pod `json:"pods"`
	// all nodes
	Nodes map[string]corev1.Node `json:"nodes"`
	// for ce
	PodsForEstimation []*corev1.Pod `json:"pods_for_estimation"`
	// for cc
	NodesToScaleDown     []string `json:"nodes_to_scale_down"`
	SelectNodeCount      int      `json:"select_node_count"`
	SchedulerCount       int      `json:"scheduler_count"`
	FailedSchedulerCount int      `json:"failed_scheduler_count"`
	// stop reason
	StopReason string `json:"stop_reason"`
}

func (s *Status) SelectNodeCountInc() {
	s.SelectNodeCount++
}

func (s *Status) SchedulerCountInc() {
	s.SchedulerCount++
}

func (s *Status) FailedSchedulerCountInc() {
	s.FailedSchedulerCount++
}
