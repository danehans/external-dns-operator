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
	// namespace limits the source of endpoints for creating ExternalDNS
	// resource records to the specified namespace.
	// If empty, defaults to the default namespace (none).
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// sources limits resource types that are queried for endpoints
	// of the given namespace.
	// If empty, defaults to Kubernetes service and ingress source types.
	// +optional
	Sources []*SourceType `json:"sources,omitempty"`

	// publicZoneFilters is one or more Public DNS zones to filter when managing
	// external DNS resource records.
	// If empty, defaults to spec.privateZone from dns.config/cluster.
	// +optional
	PublicZoneFilters []configv1.DNSZone `json:"publicZoneFilters,omitempty"`

	// privateZoneFilters is one or more Private DNS zones to filter when managing
	// external DNS resource records.
	// If empty, defaults to spec.publicZone from dns.config/cluster.
	// +optional
	PrivateZoneFilters []configv1.DNSZone `json:"privateZoneFilters,omitempty"`
}

// ServiceType is a way to restrict the type of source resources used for
// creating external DNS resource records by the ExternalDNS controller.
type SourceType string

const (
	// ServiceType limits the ExternalDNS controller to a Kubernetes
	// Service resource.
	ServiceType SourceType = "service"

	// IngressType limits the ExternalDNS controller to Kubernetes
	// Ingress resource.
	IngressType SourceType = "ingress"
)

type ExternalDNSStatus struct {
	// provider is the DNS provider where DNS records will be created.
	// Taken from infrastructure.config.openshift.io/v1
	Provider string `json:"provider"`

	// baseDomain is the domain where DNS resource records are created.
	// All records managed by ExternalDNS are sub-domains of this base.
	//
	// For example, given the base domain `openshift.example.com`, an API server
	// DNS record may be created for `api.openshift.example.com`.
	BaseDomain string `json:"baseDomain"`

	Conditions []OperatorCondition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ExternalDNSList contains a list of ExternalDNS
type ExternalDNSList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalDNS `json:"items"`
}
