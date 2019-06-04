package main

import (
	"context"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/danehans/external-dns-operator/pkg/operator"
	operatorclient "github.com/danehans/external-dns-operator/pkg/operator/client"
	operatorconfig "github.com/danehans/external-dns-operator/pkg/operator/config"
	"github.com/danehans/external-dns-operator/pkg/operator/controller"

	configv1 "github.com/openshift/api/config/v1"

	operatorv1 "github.com/danehans/api/operator/v1"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/types"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/sirupsen/logrus"

	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

const (
	// cloudCredentialsSecretName is the name of the secret in the
	// operator's namespace that will hold the credentials that the operator
	// will use to authenticate with the cloud API.
	cloudCredentialsSecretName = "cloud-credentials"
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

	creds := &corev1.Secret{}
	var provider operatorv1.ProviderType
	switch infraConfig.Status.Platform {
	case configv1.AWSPlatformType:
		// Get Operand creds
		err := kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: operatorNamespace, Name: cloudCredentialsSecretName}, creds)
		if err != nil {
			logrus.Fatalf("failed to get aws credentials from secret %q: %v", cloudCredentialsSecretName, err)
		}
		provider = operatorv1.AWSProvider
	}

	operatorConfig := operatorconfig.Config{
		OperatorReleaseVersion: releaseVersion,
		Namespace:              operatorNamespace,
		ExternalDNSImage:       externalDNSImage,
		Credentials:            creds,
		Provider:               provider,
	}

	// Set up and start the operator.
	op, err := operator.New(operatorConfig, kubeConfig, dnsConfig)
	if err != nil {
		logrus.Fatalf("failed to create operator: %v", err)
	}
	if err := op.Start(signals.SetupSignalHandler()); err != nil {
		logrus.Fatalf("failed to start operator: %v", err)
	}
}
