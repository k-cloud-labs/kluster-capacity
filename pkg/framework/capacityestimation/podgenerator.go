package capacityestimation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type singlePodGenerator struct {
	counter     uint
	podTemplate *corev1.Pod
}

func NewSinglePodGenerator(podTemplate *corev1.Pod) PodGenerator {
	return &singlePodGenerator{
		counter:     0,
		podTemplate: podTemplate,
	}
}

func (g *singlePodGenerator) Generate() *corev1.Pod {
	pod := utils.InitPod(g.podTemplate)
	// use simulated pod name with an index to construct the name
	pod.ObjectMeta.Name = fmt.Sprintf("%v-%v", g.podTemplate.Name, g.counter)

	// Ensures uniqueness
	g.counter++

	return pod
}
