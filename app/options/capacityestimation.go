package options

import (
	"context"
	"errors"
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
)

type CapacityEstimationOptions struct {
	PodTemplates    string
	PodFromClusters NamespaceNames
	SchedulerConfig string
	OutputFormat    string
	KubeConfig      string
	MaxLimit        int
	Verbose         bool
	ExcludeNodes    []string
}

type NamespaceNames []*NamespaceName

type NamespaceName struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func (n NamespaceNames) Set(nns string) error {
	for _, nn := range strings.Split(nns, ",") {
		nnStrs := strings.Split(nn, "/")
		if len(nnStrs) == 1 {
			n = append(n, &NamespaceName{
				Namespace: metav1.NamespaceDefault,
				Name:      nnStrs[0],
			})
		} else if len(nnStrs) == 2 {
			n = append(n, &NamespaceName{
				Namespace: nnStrs[0],
				Name:      nnStrs[1],
			})
		} else {
			return errors.New("invalid format")
		}
	}

	return nil
}

func (n NamespaceNames) String() string {
	strs := []string{}
	for _, nn := range n {
		strs = append(strs, fmt.Sprintf("%s/%s", nn.Namespace, nn.Name))
	}

	return strings.Join(strs, ",")
}

func (n NamespaceNames) Type() string {
	return "namespaceNames"
}

type CapacityEstimationConfig struct {
	Pod        []*corev1.Pod
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
	return &CapacityEstimationOptions{}
}

func (s *CapacityEstimationOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to the kubeconfig file to use for the analysis.")
	fs.StringVar(&s.PodTemplates, "pod-templates", s.PodTemplates, "Path to JSON or YAML file containing pod definition. Comma seperated and Exclusive with --pod-from-clusters")
	fs.Var(&s.PodFromClusters, "pod-from-clusters", "Namespace/Name of the pod from existing cluster. Comma seperated and Exclusive with --pod-templates")
	fs.IntVar(&s.MaxLimit, "max-limit", 0, "Number of instances of pod to be scheduled after which analysis stops. By default unlimited.")
	fs.StringVar(&s.SchedulerConfig, "scheduler-config", s.SchedulerConfig, "Path to JSON or YAML file containing scheduler configuration.")
	fs.BoolVar(&s.Verbose, "verbose", s.Verbose, "Verbose mode")
	fs.StringVarP(&s.OutputFormat, "output", "o", s.OutputFormat, "Output format. One of: json|yaml (Note: output is not versioned or guaranteed to be stable across releases).")
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

	if len(s.Options.PodTemplates) != 0 {
		for _, template := range strings.Split(s.Options.PodTemplates, ",") {
			pod, err := getPodFromTemplate(template)
			if err != nil {
				return err
			}
			s.Pod = append(s.Pod, pod)
		}
	} else {
		for _, nn := range s.Options.PodFromClusters {
			pod, err := s.KubeClient.CoreV1().Pods(nn.Namespace).Get(context.TODO(), nn.Name, metav1.GetOptions{ResourceVersion: "0"})
			if err != nil {
				return err
			}
			s.Pod = append(s.Pod, pod)
		}
	}

	return nil
}
