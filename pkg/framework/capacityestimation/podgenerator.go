package capacityestimation

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	apiv1 "k8s.io/kubernetes/pkg/apis/core/v1"

	pkgframework "github.com/k-cloud-labs/kluster-capacity/pkg/framework"
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
	pod := g.podTemplate.DeepCopy()

	apiv1.SetObjectDefaults_Pod(pod)

	// reset pod
	pod.Spec.NodeName = ""
	pod.Namespace = pkgframework.Namespace
	pod.Spec.SchedulerName = pkgframework.SchedulerName
	pod.Status = corev1.PodStatus{}

	// use simulated pod name with an index to construct the name
	pod.ObjectMeta.Name = fmt.Sprintf("%v-%v", g.podTemplate.Name, g.counter)
	pod.ObjectMeta.UID = types.UID(uuid.NewV4().String())

	// Add pod provisioner annotation
	if pod.ObjectMeta.Annotations == nil {
		pod.ObjectMeta.Annotations = map[string]string{}
	}
	pod.ObjectMeta.Annotations[pkgframework.PodProvisioner] = pkgframework.SchedulerName

	// Ensures uniqueness
	g.counter++

	return pod
}
