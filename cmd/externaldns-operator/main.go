package main

import (
	"context"
	"github.com/openshift/cluster-ingress-operator/pkg/operator/controller"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/danehans/external-dns-operator/pkg/operator"
	operatorclient "github.com/danehans/external-dns-operator/pkg/operator/client"
	operatorconfig "github.com/danehans/external-dns-operator/pkg/operator/config"

	configv1 "github.com/openshift/api/config/v1"

	"k8s.io/apimachinery/pkg/types"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/sirupsen/logrus"

	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

func main() {
	// Get a kube client.
	kubeConfig, err := config.GetConfig()
	if err != nil {
		logrus.Fatalf("failed to get kube config: %v", err)
	}
	kubeClient, err := operatorclient.NewClient(kubeConfig)
	if err != nil {
		logrus.Fatalf("failed to create kube client: %v", err)
	}

	// Collect operator configuration.
	operatorNamespace := os.Getenv("WATCH_NAMESPACE")
	if len(operatorNamespace) == 0 {
		logrus.Fatalf("WATCH_NAMESPACE environment variable is required")
		os.Exit(1)
	}
	externalDNSImage := os.Getenv("IMAGE")
	if len(externalDNSImage) == 0 {
		logrus.Fatalf("IMAGE environment variable is required")
	}
	releaseVersion := os.Getenv("RELEASE_VERSION")
	if len(releaseVersion) == 0 {
		releaseVersion = controller.UnknownReleaseVersionName
		logrus.Infof("RELEASE_VERSION environment variable missing; using release version: %s", controller.UnknownReleaseVersionName)
	}

	// Retrieve the cluster infrastructure and dns configs.
	infraConfig := &configv1.Infrastructure{}
	err = kubeClient.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, infraConfig)
	if err != nil {
		logrus.Fatalf("failed to get infrastructure 'config': %v", err)
	}
	dnsConfig := &configv1.DNS{}
	err = kubeClient.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, dnsConfig)
	if err != nil {
		logrus.Fatalf("failed to get dns 'cluster': %v", err)
	}

	operatorConfig := operatorconfig.Config{
		OperatorReleaseVersion: releaseVersion,
		Namespace:              operatorNamespace,
		ExternalDNSImage:       externalDNSImage,
	}

	// Set up and start the operator.
	op, err := operator.New(operatorConfig)
	if err != nil {
		logrus.Fatalf("failed to create operator: %v", err)
	}
	if err := op.Start(signals.SetupSignalHandler()); err != nil {
		logrus.Fatalf("failed to start operator: %v", err)
	}
}
