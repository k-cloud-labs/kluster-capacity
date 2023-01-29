package options

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	api "k8s.io/kubernetes/pkg/apis/core"
	apiv1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/apis/core/validation"
)

type CapacityEstimationOptions struct {
	PodTemplate     string
	PodFromCluster  string
	Namespace       string
	Name            string
	SchedulerConfig string
	OutputFormat    string
	KubeConfig      string
	MaxLimit        int
	// key=v#key=v#key=v, key is resource name and v is resource value
	ResourceList []string
	Verbose      bool
	ExcludeNodes []string
}

type CapacityEstimationConfig struct {
	Pod        *corev1.Pod
	KubeClient clientset.Interface
	// TODO: try to use fake dynamicInformerFactory
	RestConfig *restclient.Config
	Options    *CapacityEstimationOptions
}

func NewCapacityEstimationConfig(opt *CapacityEstimationOptions) *CapacityEstimationConfig {
	return &CapacityEstimationConfig{
		Options: opt,
	}
}

func NewCapacityEstimationOptions() *CapacityEstimationOptions {
	return &CapacityEstimationOptions{
		ResourceList: make([]string, 0),
	}
}

func (s *CapacityEstimationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kubeconfig file to use for the analysis.")
	fs.StringVar(&s.PodTemplate, "pod-template", s.PodTemplate, "Path to JSON or YAML file containing pod definition. Exclusive with --pod-from-cluster")
	fs.StringVar(&s.PodFromCluster, "pod-from-cluster", s.PodFromCluster, "Namespace/Name of the pod from existing cluster. Exclusive with --pod-template")
	fs.IntVar(&s.MaxLimit, "max-limit", 0, "Number of instances of pod to be scheduled after which analysis stops. By default unlimited.")
	fs.StringSliceVar(&s.ResourceList, "resource-list", s.ResourceList, "Resource list used for pod to schedule to return the result in batches.")
	fs.StringVar(&s.SchedulerConfig, "scheduler-config", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration.")
	fs.BoolVar(&s.Verbose, "verbose", s.Verbose, "Verbose mode")
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|yaml (Note: output is not versioned or guaranteed to be stable across releases).")
	fs.StringSliceVar(&s.ExcludeNodes, "exclude-nodes", s.ExcludeNodes, "Exclude nodes to be scheduled")
}

func (s *CapacityEstimationConfig) ParseAPISpec() error {
	var (
		err          error
		versionedPod = &corev1.Pod{}
	)

	if len(s.Options.PodTemplate) != 0 {
		var spec io.Reader

		if strings.HasPrefix(s.Options.PodTemplate, "http://") || strings.HasPrefix(s.Options.PodTemplate, "https://") {
			response, err := http.Get(s.Options.PodTemplate)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("unable to read URL %q, server reported %v, status code=%v", s.Options.PodTemplate, response.Status, response.StatusCode)
			}
			spec = response.Body
		} else {
			filename, _ := filepath.Abs(s.Options.PodTemplate)
			spec, err = os.Open(filename)
			if err != nil {
				return fmt.Errorf("failed to open config file: %v", err)
			}
		}

		decoder := yaml.NewYAMLOrJSONDecoder(spec, 4096)
		err = decoder.Decode(versionedPod)
		if err != nil {
			return fmt.Errorf("failed to decode config file: %v", err)
		}
	} else {
		versionedPod, err = s.KubeClient.CoreV1().Pods(s.Options.Namespace).Get(context.TODO(), s.Options.Name, metav1.GetOptions{ResourceVersion: "0"})
		if err != nil {
			return err
		}
	}

	if versionedPod.ObjectMeta.Namespace == "" {
		versionedPod.ObjectMeta.Namespace = "default"
	}

	apiv1.SetObjectDefaults_Pod(versionedPod)

	internalPod := &api.Pod{}
	if err := apiv1.Convert_v1_Pod_To_core_Pod(versionedPod, internalPod, nil); err != nil {
		return fmt.Errorf("unable to convert to internal version: %#v", err)
	}
	if errs := validation.ValidatePodCreate(internalPod, validation.PodValidationOptions{}); len(errs) > 0 {
		var errStrs []string
		for _, err := range errs {
			errStrs = append(errStrs, fmt.Sprintf("%v: %v", err.Type, err.Field))
		}
		return fmt.Errorf("invalid pod: %#v", strings.Join(errStrs, ", "))
	}

	s.Pod = versionedPod
	return nil
}
