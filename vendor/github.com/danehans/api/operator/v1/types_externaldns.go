package v1

import (
	configv1 "github.com/openshift/api/config/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalDNS describes a managed ExternalDNS controller for an OpenShift cluster.
// The controller supports OpenShift Service [1] and Ingress [2] resources.
//
// [1] https://kubernetes.io/docs/concepts/services-networking/service
// [2] https://kubernetes.io/docs/concepts/services-networking/ingress
//
// When an ExternalDNS is created, a new ExternalDNS controller is instantiated
// within the OpenShift cluster. The controller provides dns resource record management
// of specific service and/or ingress resources for the configured OpenShift platform
// type.
//
// Whenever possible, sensible defaults are used. See each field for more details.
type ExternalDNS struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the ExternalDNS.
	Spec ExternalDNSSpec `json:"spec,omitempty"`
	// status is the most recently observed status of the ExternalDNS.
	Status ExternalDNSStatus `json:"status,omitempty"`
}

type ExternalDNSSpec struct {
	// baseDomain is the base domain used for creating resource records.
	// For example, given the base domain `openshift.example.com`, an API
	// server record may be created for `api.openshift.example.com`.
	//
	// baseDomain must be unique among all ExternalDNSes and cannot be
	// updated.
	//
	// If empty, defaults to dns.config/cluster .spec.baseDomain.
	//
	// +optional
	BaseDomain string `json:"baseDomain,omitempty"`

	// namespace limits the source of endpoints for creating ExternalDNS
	// resource records to the specified namespace.
	//
	// If empty, defaults to all namespaces.
	//
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// sources limits resource types that are queried for endpoints
	// of the given namespace.
	//
	// If empty, defaults to a Kubernetes Service resource type.
	//
	// +optional
	Sources []*SourceType `json:"sources,omitempty"`

	// provider is the specification of the DNS provider where DNS records
	// will be created.
	//
	// +optional
	Provider ProviderSpec `json:"provider,omitempty"`
}

// sourceType is a way to restrict the type of source resources used for
// creating resource records by the ExternalDNS controller.
type SourceType string

const (
	// serviceType limits sources for creating records to the Kubernetes
	// Service resource type.
	ServiceType SourceType = "service"
)

type ProviderSpec struct {
	// name is the service provider name used for creating resource records.
	//
	// If empty, defaults to infrastructure.config/cluster .status.platform.
	//
	// +optional
	Name ProviderName `json:"name,omitempty"`

	// zoneIDFilter is a comma separated list of target DNS zone
	// IDs to include for managing external DNS resource records.
	//
	// If empty, defaults to dns.config/cluster .spec.privateZone.
	//
	// +optional
	ZoneIDFilter []configv1.DNSZone `json:"zoneIDFilter,omitempty"`

	// args is the list of configuration arguments used for the provider.
	//
	// If empty, no arguments are used for the provider.
	//
	// +optional
	Args []string `json:"args,omitempty"`
}

// ProviderName specifies the name of external DNS provider to use
// for creating resource records.
type ProviderName string

const (
	// awsProvider is the name of the Amazon Web Services Route 53 DNS
	// service provider.
	// https://aws.amazon.com/route53
	awsProvider ProviderName = "aws"

	// azureProvider is the name of the Azure DNS service provider.
	// https://docs.microsoft.com/en-us/azure/dns/
	azureProvider ProviderName = "azure"

	// googleProvider is the name of the Google Cloud DNS service provider.
	// https://cloud.google.com/dns/
	googleProvider ProviderName = "google"
)

type ExternalDNSStatus struct {
	// provider is the name of the DNS provider in use.
	Provider string `json:"provider"`

	// baseDomain is the base domain in use for creating resource records.
	BaseDomain string `json:"baseDomain"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalDNSList contains a list of ExternalDNS
type ExternalDNSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalDNS `json:"items"`
}
