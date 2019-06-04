package config

import (
	operatorv1 "github.com/danehans/api/operator/v1"

	corev1 "k8s.io/api/core/v1"
)

// Config is configuration for the operator and should include things like
// operated images, release version, etc.
type Config struct {
	// OperatorReleaseVersion is the current version of operator.
	OperatorReleaseVersion string

	// Namespace is the operator namespace.
	Namespace string

	// ExternalDNSImage is the CoreDNS image to manage.
	ExternalDNSImage string

	// Credentials is the Kubernetes secret containing the cloud
	// provider authentication credentials.
	Credentials *corev1.Secret

	// Provider is the cloud provider running the OpenShift cluster.
	Provider operatorv1.ProviderType
}
