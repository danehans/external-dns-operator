package controller

import (
	operatorv1 "github.com/danehans/api/operator/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// controllerDeploymentLabel identifies a deployment as an
	// externaldns deployment, and the value is the name of the
	// owning externaldns.
	controllerDeploymentLabel = "externaldns.operator.openshift.io/deployment-externaldns"
)

// ExternalDNSDeploymentName returns the namespaced name
// for the externaldns Deployment.
func ExternalDNSDeploymentName(edns *operatorv1.ExternalDNS) types.NamespacedName {
	return types.NamespacedName{
		Namespace: "openshift-externaldns",
		Name:      "externaldns-" + edns.Name,
	}
}

func ExternalDNSDeploymentLabel(edns *operatorv1.ExternalDNS) string {
	return edns.Name
}

func ExternalDNSDeploymentPodSelector(edns *operatorv1.ExternalDNS) *metav1.LabelSelector {
	return &metav1.LabelSelector{
		MatchLabels: map[string]string{
			controllerDeploymentLabel: ExternalDNSDeploymentLabel(edns),
		},
	}
}
