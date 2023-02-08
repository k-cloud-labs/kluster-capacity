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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/k-cloud-labs/kluster-capacity/pkg/utils"
)

type CapacityEstimationOptions struct {
	PodsFromTemplate []string
	PodsFromCluster  NamespaceNames
	SchedulerConfig  string
	OutputFormat     string
	KubeConfig       string
	MaxLimit         int
	Verbose          bool
	ExcludeNodes     []string
	SaveTo           string
}

type CapacityEstimationConfig struct {
	Pods     []*corev1.Pod
	InitObjs []runtime.Object
	Options  *CapacityEstimationOptions
}

func NewCapacityEstimationConfig(opt *CapacityEstimationOptions) *CapacityEstimationConfig {
	return &CapacityEstimationConfig{
		Options: opt,
	}
}

func NewCapacityEstimationOptions() *CapacityEstimationOptions {
	return &CapacityEstimationOptions{}
}

func (s *CapacityEstimationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kubeconfig file to use for the analysis")
	fs.StringSliceVar(&s.PodsFromTemplate, "pods-from-template", s.PodsFromTemplate, "Path to JSON or YAML file containing pod definition. Comma seperated and Exclusive with --pods-from-cluster")
	fs.Var(&s.PodsFromCluster, "pods-from-cluster", "Namespace/Name of the pod from existing cluster. Comma seperated and Exclusive with --pods-from-template")
	fs.IntVar(&s.MaxLimit, "max-limit", 0, "Number of instances of pod to be scheduled after which analysis stops. By default unlimited")
	fs.StringVar(&s.SchedulerConfig, "scheduler-config", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration")
	fs.BoolVar(&s.Verbose, "verbose", s.Verbose, "Verbose mode")
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|yaml (Note: output is not versioned or guaranteed to be stable across releases)")
	fs.StringSliceVar(&s.ExcludeNodes, "exclude-nodes", s.ExcludeNodes, "Exclude nodes to be scheduled")
}

func (s *CapacityEstimationConfig) ParseAPISpec() error {
	getPodFromTemplate := func(template string) (*corev1.Pod, error) {
		var (
			err          error
			versionedPod = &corev1.Pod{}
			spec         io.Reader
		)

		if strings.HasPrefix(template, "http://") || strings.HasPrefix(template, "https://") {
			response, err := http.Get(template)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("unable to read URL %q, server reported %v, status code=%v", template, response.Status, response.StatusCode)
			}
			spec = response.Body
		} else {
			filename, _ := filepath.Abs(template)
			spec, err = os.Open(filename)
			if err != nil {
				return nil, fmt.Errorf("failed to open config file: %v", err)
			}
		}

		decoder := yaml.NewYAMLOrJSONDecoder(spec, 4096)
		err = decoder.Decode(versionedPod)
		if err != nil {
			return nil, fmt.Errorf("failed to decode config file: %v", err)
		}

		return versionedPod, nil
	}

	if len(s.Options.PodsFromTemplate) != 0 {
		for _, template := range s.Options.PodsFromTemplate {
			pod, err := getPodFromTemplate(template)
			if err != nil {
				return err
			}
			s.Pods = append(s.Pods, pod)
		}
	} else {
		cfg, err := utils.BuildRestConfig(s.Options.KubeConfig)
		if err != nil {
			return err
		}

		kubeClient, err := clientset.NewForConfig(cfg)
		if err != nil {
			return err
		}

		for _, nn := range s.Options.PodsFromCluster {
			pod, err := kubeClient.CoreV1().Pods(nn.Namespace).Get(context.TODO(), nn.Name, metav1.GetOptions{ResourceVersion: "0"})
			if err != nil {
				return err
			}
			s.Pods = append(s.Pods, pod)
		}
	}

	return nil
}
