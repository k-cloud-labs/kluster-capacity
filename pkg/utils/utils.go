package utils

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/events"
	configv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/component-base/logs"
	kubeschedulerconfigv1 "k8s.io/kube-scheduler/config/v1beta1"
	schedconfig "k8s.io/kubernetes/cmd/kube-scheduler/app/config"
	kubescheduleroptions "k8s.io/kubernetes/cmd/kube-scheduler/app/options"
	kubeschedulerconfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	kubeschedulerscheme "k8s.io/kubernetes/pkg/scheduler/apis/config/scheme"
	"k8s.io/kubernetes/pkg/scheduler/apis/config/validation"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/k-cloud-labs/kluster-capacity/pkg"
)

const (
	DefaultQPS   = 10000
	DefaultBurst = 20000
)

func BuildRestConfig(config string) (*restclient.Config, error) {
	if len(config) != 0 {
		master, err := getMasterFromKubeConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig file: %v ", err)
		}

		cfg, err := clientcmd.BuildConfigFromFlags(master, config)
		if err != nil {
			return nil, fmt.Errorf("unable to build config: %v", err)
		}

		cfg.QPS = DefaultQPS
		cfg.Burst = DefaultBurst

		return cfg, nil
	} else {
		cfg, err := restclient.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("unable to build in cluster config: %v", err)
		}

		cfg.QPS = DefaultQPS
		cfg.Burst = DefaultBurst

		return cfg, nil
	}
}

func BuildKubeSchedulerCompletedConfig(config, kubeconfig string) (*schedconfig.CompletedConfig, error) {
	var kcfg *kubeschedulerconfig.KubeSchedulerConfiguration
	if len(config) > 0 {
		cfg, err := loadConfigFromFile(config)
		if err != nil {
			return nil, err
		}
		if err := validation.ValidateKubeSchedulerConfiguration(cfg); len(err) > 0 {
			return nil, err.ToAggregate()
		}
		kcfg = cfg
	}

	if len(kcfg.ClientConnection.Kubeconfig) == 0 && len(kubeconfig) > 0 {
		kcfg.ClientConnection.Kubeconfig = kubeconfig
	}

	cc, err := buildKubeSchedulerCompletedConfig(kcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init kube scheduler configuration: %v ", err)
	}

	return cc, nil
}

func PrintJson(r pkg.Printer) error {
	jsonBytes, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to create json: %v", err)
	}
	fmt.Println(string(jsonBytes))
	return nil
}

func PrintYaml(r pkg.Printer) error {
	yamlBytes, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to create yaml: %v", err)
	}
	fmt.Print(string(yamlBytes))
	return nil
}

func ComputePodResourceRequest(pod *corev1.Pod) *framework.Resource {
	result := &framework.Resource{}

	for _, container := range pod.Spec.Containers {
		result.Add(container.Resources.Requests)
	}

	// take max_resource(sum_pod, any_init_container)
	for _, container := range pod.Spec.InitContainers {
		result.SetMaxResource(container.Resources.Requests)
	}

	// If Overhead is being utilized, add to the total requests for the pod
	if pod.Spec.Overhead != nil {
		result.Add(pod.Spec.Overhead)
	}
	return result
}

func buildKubeSchedulerCompletedConfig(kcfg *kubeschedulerconfig.KubeSchedulerConfiguration) (*schedconfig.CompletedConfig, error) {
	if kcfg == nil {
		kcfg = &kubeschedulerconfig.KubeSchedulerConfiguration{}
		versionedCfg := kubeschedulerconfigv1.KubeSchedulerConfiguration{}
		versionedCfg.DebuggingConfiguration = *configv1alpha1.NewRecommendedDebuggingConfiguration()

		kubeschedulerscheme.Scheme.Default(&versionedCfg)
		if err := kubeschedulerscheme.Scheme.Convert(&versionedCfg, kcfg, nil); err != nil {
			return nil, err
		}
	}

	// inject scheduler config
	if len(kcfg.Profiles) == 0 {
		kcfg.Profiles = []kubeschedulerconfig.KubeSchedulerProfile{
			{},
		}
	}

	kcfg.Profiles[0].SchedulerName = pkg.SchedulerName
	if kcfg.Profiles[0].Plugins == nil {
		kcfg.Profiles[0].Plugins = &kubeschedulerconfig.Plugins{}
	}

	opts := &kubescheduleroptions.Options{
		ComponentConfig: *kcfg,
		Logs:            logs.NewOptions(),
	}

	c := &schedconfig.Config{}
	// clear out all unnecessary options so no port is bound
	// to allow running multiple instances in a row
	opts.Deprecated = nil
	opts.SecureServing = nil
	if err := opts.ApplyTo(c); err != nil {
		return nil, fmt.Errorf("unable to get scheduler kcfg: %v", err)
	}

	// Get the completed config
	cc := c.Complete()

	// completely ignore the events
	cc.EventBroadcaster = events.NewEventBroadcasterAdapter(fakeclientset.NewSimpleClientset())

	// black magic
	cc.Client = fakeclientset.NewSimpleClientset()
	cc.InformerFactory = informers.NewSharedInformerFactory(cc.Client, 0)

	return &cc, nil
}

func loadConfigFromFile(file string) (*kubeschedulerconfig.KubeSchedulerConfiguration, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return loadConfig(data)
}

func loadConfig(data []byte) (*kubeschedulerconfig.KubeSchedulerConfiguration, error) {
	// The UniversalDecoder runs defaulting and returns the internal type by default.
	obj, gvk, err := kubeschedulerscheme.Codecs.UniversalDecoder().Decode(data, nil, nil)
	if err != nil {
		return nil, err
	}
	if cfgObj, ok := obj.(*kubeschedulerconfig.KubeSchedulerConfiguration); ok {
		// We don't set this field in pkg/scheduler/apis/config/{version}/conversion.go
		// because the field will be cleared later by API machinery during
		// conversion. See KubeSchedulerConfiguration internal type definition for
		// more details.
		cfgObj.TypeMeta.APIVersion = gvk.GroupVersion().String()
		return cfgObj, nil
	}
	return nil, fmt.Errorf("couldn't decode as KubeSchedulerConfiguration, got %s: ", gvk)
}

func getMasterFromKubeConfig(filename string) (string, error) {
	config, err := clientcmd.LoadFromFile(filename)
	if err != nil {
		return "", fmt.Errorf("can not load kubeconfig file: %v", err)
	}

	context, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "", fmt.Errorf("failed to get master address from kubeconfig")
	}

	if val, ok := config.Clusters[context.Cluster]; ok {
		return val.Server, nil
	}
	return "", fmt.Errorf("failed to get master address from kubeconfig")
}
