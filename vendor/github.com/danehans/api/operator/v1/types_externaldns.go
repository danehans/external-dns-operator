package v1

import (
	configv1 "github.com/openshift/api/config/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//
// ExternalDNS describes a managed ExternalDNS controller for an OpenShift cluster.
// The controller supports the Kubernetes Service [1] resource:
//
// [1] https://kubernetes.io/docs/concepts/services-networking/service
//
// When an ExternalDNS is created, a new ExternalDNS controller is instantiated
// within the OpenShift cluster. The controller provides dns resource record management
// of specific service resources for the configured OpenShift platform.
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

	// zoneType...
	//
	// If empty, defaults to PrivateZoneType.
	//
	// +optional
	ZoneType *ZoneType `json:"zoneType,omitempty"`

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

// zoneType...
type ZoneType string

const (
	// publicZoneType...
	PublicZoneType ZoneType = "public"

	// privateType...
	PrivateZoneType ZoneType = "private"
)

type ProviderSpec struct {
	// type is the ExternalDNS provider used for creating resource records.
	//
	// If empty, defaults to infrastructure.config/cluster .status.platform.
	//
	// +optional
	Type *ProviderType `json:"type,omitempty"`

	// zoneFilter is a comma separated list of target DNSZone's
	// to include for managing external DNS resource records.
	//
	// If empty, defaults to dns.config/cluster .spec.privateZone.
	//
	// +optional
	ZoneFilter []*configv1.DNSZone `json:"zoneFilter,omitempty"`

	// args is the list of configuration arguments used for the provider.
	//
	// If empty, no arguments are used for the provider.
	//
	// +optional
	Args []string `json:"args,omitempty"`
}

// providerType specifies the name of external DNS provider to use
// for creating resource records.
type ProviderType string

const (
	// awsProvider is the name of the Amazon Web Services Route 53 DNS
	// ExternalDNS provider.
	//
	// https://aws.amazon.com/route53 for more details.
	AWSProvider ProviderType = "aws"

	// azureProvider is the name of the Azure DNS ExternalDNS provider.
	//
	// https://docs.microsoft.com/en-us/azure/dns for more details.
	AzureProvider ProviderType = "azure"

	// googleProvider is the name of the Google Cloud DNS
	// ExternalDNS provider.
	//
	// https://cloud.google.com/dns for more details.
	GoogleProvider ProviderType = "google"
)

type ExternalDNSStatus struct {
	// baseDomain is the baseDomain in use.
	BaseDomain string `json:"baseDomain"`

	// providerType is the type of ExternalDNS provider
	// in use.
	ProviderType *ProviderType `json:"provider,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalDNSList contains a list of ExternalDNS
type ExternalDNSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalDNS `json:"items"`
}
