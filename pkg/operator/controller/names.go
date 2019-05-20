package controller

import (
	operatorv1 "github.com/danehans/api/operator/v1"

	configv1 "github.com/openshift/api/config/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// controllerDeploymentLabel identifies a deployment as an
	// externaldns deployment, and the value is the name of the
	// owning externaldns.
	controllerDeploymentLabel = "externaldns.operator.openshift.io/deployment-externaldns"
)

// ExternalDNSDeploymentNamespacedName returns the namespaced name
// for the externaldns Deployment.
func ExternalDNSDeploymentNamespacedName(edns *operatorv1.ExternalDNS) types.NamespacedName {
	return types.NamespacedName{
		Namespace: "openshift-externaldns",
		Name:      "externaldns-" + edns.Name,
	}
}

// ExternalDNSNamespacedName returns the namespaced name of edns.
func ExternalDNSNamespacedName(edns *operatorv1.ExternalDNS) types.NamespacedName {
	return types.NamespacedName{
		Namespace: edns.Namespace,
		Name:      edns.Name,
	}
}

// ExternalDNSNamespaceName returns namespace/name string of edns.
func ExternalDNSNamespaceName(edns *operatorv1.ExternalDNS) string {
	return edns.Namespace + "/" + edns.Name
}

// ExternalDNSNamespace returns the namespace of edns as a string.
func ExternalDNSNamespace(edns *operatorv1.ExternalDNS) string {
	return edns.Namespace
}

// ExternalDNSName returns the name of edns as a string.
func ExternalDNSName(edns *operatorv1.ExternalDNS) string {
	return edns.Name
}

// TextOwnerID returns the ExternalDNS controller txt owner id.
func TextOwnerID(infraConfig *configv1.Infrastructure, edns *operatorv1.ExternalDNS) string {
	return infraConfig.Status.InfrastructureName + "/" + ExternalDNSNamespaceName(edns)
}

// ExternalDNSDeploymentPodSelector returns a LabelSelector based
// on the name of edns.
func ExternalDNSDeploymentPodSelector(edns *operatorv1.ExternalDNS) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			controllerDeploymentLabel: ExternalDNSName(edns),
		},
	}
}
