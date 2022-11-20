package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	populator_machinery "github.com/kubernetes-csi/lib-volume-populator/populator-machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	prefix     = "forklift.konveyor.io"
	mountPath  = "/mnt/"
	devicePath = "/dev/block"
)

var version = "unknown"

func main() {
	var (
		mode         string
		engineUrl    string
		secretName   string
		diskID       string
		fileName     string
		httpEndpoint string
		metricsPath  string
		masterURL    string
		kubeconfig   string
		imageName    string
		showVersion  bool
		namespace    string
	)

	// Main arg
	flag.StringVar(&mode, "mode", "", "Mode to run in (controller, populate)")
	// Populate args
	flag.StringVar(&engineUrl, "engine-url", "", "ovirt-engine url (https//engine.fqdn)")
	flag.StringVar(&secretName, "secret-name", "", "secret containing oVirt credentials")
	flag.StringVar(&diskID, "disk-id", "", "ovirt-engine disk id")
	flag.StringVar(&fileName, "file-name", "", "File name to populate")

	// Controller args
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&imageName, "image-name", "", "Image to use for populating")
	// Metrics args
	flag.StringVar(&httpEndpoint, "http-endpoint", "", "The TCP network address where the HTTP server for diagnostics, including metrics and leader election health check, will listen (example: `:8080`). The default is empty string, which means the server is disabled.")
	flag.StringVar(&metricsPath, "metrics-path", "/metrics", "The HTTP path where prometheus metrics will be exposed. Default is `/metrics`.")
	// Other args
	flag.BoolVar(&showVersion, "version", false, "display the version string")
	flag.StringVar(&namespace, "namespace", "ovirt-imageio-populator", "Namespace to deploy controller")
	flag.Parse()

	if showVersion {
		fmt.Println(os.Args[0], version)
		os.Exit(0)
	}

	switch mode {
	case "controller":
		const (
			groupName  = "forklift.konveyor.io"
			apiVersion = "v1beta1"
			kind       = "OvirtImageIOPopulator"
			resource   = "ovirtimageiopopulators"
		)
		var (
			gk  = schema.GroupKind{Group: groupName, Kind: kind}
			gvr = schema.GroupVersionResource{Group: groupName, Version: apiVersion, Resource: resource}
		)
		populator_machinery.RunController(masterURL, kubeconfig, imageName, httpEndpoint, metricsPath,
			namespace, prefix, gk, gvr, mountPath, devicePath, getPopulatorPodArgs)
	case "populate":
		populate(masterURL, kubeconfig, engineUrl, secretName, diskID, fileName, namespace)
	default:
		klog.Fatalf("Invalid mode: %s", mode)
	}
}

type OvirtImageIOPopulator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OvirtImageIOPopulatorSpec `json:"spec"`
}

type OvirtImageIOPopulatorSpec struct {
	EngineURL        string `json:"engineUrl"`
	EngineSecretName string `json:"engineSecretName"`
	DiskID           string `json:"diskId"`
}

type engineConfig struct {
	URL      string
	username string
	password string
	ca       string
}

func getSecret(secretName, engineURL, namespace string) engineConfig {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatal(err.Error())
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatal(err.Error())
	}

	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		klog.Fatal(err.Error())
	}

	return engineConfig{
		URL:      engineURL,
		username: string(secret.Data["user"]),
		password: string(secret.Data["password"]),
		ca:       string(secret.Data["cacert"]),
	}
}

func getPopulatorPodArgs(rawBlock bool, u *unstructured.Unstructured) ([]string, error) {
	var ovirtImageIOPopulator OvirtImageIOPopulator
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.UnstructuredContent(), &ovirtImageIOPopulator)
	args := []string{"--mode=populate"}
	if nil != err {
		return nil, err
	}

	if rawBlock {
		args = append(args, "--file-name="+devicePath)
	} else {
		args = append(args, "--file-name="+mountPath+"disk.img")
	}

	args = append(args, "--secret-name="+ovirtImageIOPopulator.Spec.EngineSecretName)
	args = append(args, "--disk-id="+ovirtImageIOPopulator.Spec.DiskID)
	args = append(args, "--engine-url="+ovirtImageIOPopulator.Spec.EngineURL)
	args = append(args, "--namespace=ovirt-imageio-populator")

	return args, nil
}

func populate(masterURL, kubeconfig, engineURL, secretName, diskID, fileName, namespace string) {
	engineConfig := getSecret(secretName, engineURL, namespace)

	// Write credentials to files
	ovirtPass, err := os.Create("/tmp/ovirt.pass")
	if err != nil {
		klog.Fatalf("Failed to create ovirt.pass %s", err)
	}

	defer ovirtPass.Close()
	if err != nil {
		klog.Fatalf("Failed to create file %s", err)
	}
	ovirtPass.Write([]byte(engineConfig.password))

	cert, err := os.Create("/tmp/ca.pem")
	if err != nil {
		klog.Fatalf("Failed to create ca.pem %s", err)
	}

	defer cert.Close()
	if err != nil {
		klog.Fatalf("Failed to create file %s", err)
	}

	cert.Write([]byte(engineConfig.ca))

	args := []string{
		"download-disk",
		"--engine-url=" + engineConfig.URL,
		"--username=" + engineConfig.username,
		"--password-file=/tmp/ovirt.pass",
		"--cafile=" + "/tmp/ca.pem",
		"-f", "raw",
		diskID,
		fileName,
	}
	cmd := exec.Command("ovirt-img", args...)
	output, err := cmd.CombinedOutput()
	fmt.Printf("%s\n", output)
	if err != nil {
		klog.Fatal(err)
	}
}
