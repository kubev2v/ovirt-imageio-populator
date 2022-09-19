package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	populator_machinery "github.com/kubernetes-csi/lib-volume-populator/populator-machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		mode           string
		engineUrl      string
		engineUser     string
		enginePassword string
		engineCA       string
		diskID         string
		fileName       string
		httpEndpoint   string
		metricsPath    string
		masterURL      string
		kubeconfig     string
		imageName      string
		showVersion    bool
		namespace      string
	)
	// Main arg
	flag.StringVar(&mode, "mode", "", "Mode to run in (controller, populate)")
	// Populate args
	flag.StringVar(&engineUrl, "engine-url", "", "ovirt-engine url (https//engine.fqdn)")
	flag.StringVar(&engineUser, "engine-user", "", "ovirt-engine user (admin@ovirt@internalsso)")
	flag.StringVar(&enginePassword, "engine-password", "", "ovirt-engine password")
	flag.StringVar(&engineCA, "ca", "", "CA file for imageio")
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
	flag.StringVar(&namespace, "namespace", "default", "Namespace to deploy controller")
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
		populate(masterURL, kubeconfig, engineUrl, engineUser, enginePassword, engineCA, diskID, fileName)
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
	EngineURL      string `json:"engineUrl"`
	EngineUser     string `json:"engineUser"`
	EnginePassword string `json:"enginePassword"`
	EngineCA       string `json:"engineCA"`
	DiskID         string `json:"diskId"`
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
		args = append(args, "--file-name="+mountPath)
	}

	args = append(args, "--engine-url="+ovirtImageIOPopulator.Spec.EngineURL)
	args = append(args, "--engine-user="+ovirtImageIOPopulator.Spec.EngineUser)
	args = append(args, "--engine-password="+ovirtImageIOPopulator.Spec.EnginePassword)
	args = append(args, "--ca="+ovirtImageIOPopulator.Spec.EngineCA)
	args = append(args, "--disk-id="+ovirtImageIOPopulator.Spec.DiskID)

	return args, nil
}

func populate(masterURL, kubeconfig, engineUrl, engineUser, enginePassword, ca, diskID, fileName string) {
	// TODO handle block device

	// Write credentials to files
	ovirtPass, err := os.Create("/tmp/ovirt.pass")
	if err != nil {
		klog.Fatalf("Failed to create ovirt.pass %s", err)
	}

	defer ovirtPass.Close()
	if err != nil {
		klog.Fatalf("Failed to create file %s", err)
	}
	ovirtPass.Write([]byte(enginePassword))

	cert, err := os.Create("/tmp/ca.pem")
	if err != nil {
		klog.Fatalf("Failed to create ca.pem %s", err)
	}

	defer cert.Close()
	if err != nil {
		klog.Fatalf("Failed to create file %s", err)
	}

	cert.Write([]byte(ca))

	args := []string{"download-disk",
		"--engine-url=" + engineUrl,
		"--username=" + engineUser,
		"--password-file=/tmp/ovirt.pass",
		"--cafile=" + "/tmp/ca.pem",
		"-f", "raw",
		diskID,
		fileName + "disk.img"} // TODO we don't need "disk.img" if it's a block device
	cmd := exec.Command("ovirt-img", args...)
	output, err := cmd.CombinedOutput()
	fmt.Printf("%s\n", output)
	if err != nil {
		klog.Fatal(err)
	}
}
