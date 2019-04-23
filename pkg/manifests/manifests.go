package manifests

import (
	"bytes"
	"io"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	ExternalDNSNamespaceAsset          = "assets/externaldns/namespace.yaml"
	ExternalDNSServiceAccountAsset     = "assets/externaldns/service-account.yaml"
	ExternalDNSClusterRoleAsset        = "assets/externaldns/cluster-role.yaml"
	ExternalDNSClusterRoleBindingAsset = "assets/externaldns/cluster-role-binding.yaml"
	ExternalDNSDeploymentAsset         = "assets/externaldns/deployment.yaml"

	// OwningExternalDNSLabel should be applied to any objects "owned by"
	// a dns to aid in selection (especially in cases where an ownerref
	// can't be established due to namespace boundaries).
	OwningExternalDNSLabel = "externaldns.operator.openshift.io/owning-externaldns"
)

func MustAssetReader(asset string) io.Reader {
	return bytes.NewReader(MustAsset(asset))
}

func ExternalDNSNamespace() *corev1.Namespace {
	ns, err := NewNamespace(MustAssetReader(ExternalDNSNamespaceAsset))
	if err != nil {
		panic(err)
	}
	return ns
}

func ExternalDNSServiceAccount() *corev1.ServiceAccount {
	sa, err := NewServiceAccount(MustAssetReader(ExternalDNSServiceAccountAsset))
	if err != nil {
		panic(err)
	}
	return sa
}

func ExternalDNSClusterRole() *rbacv1.ClusterRole {
	cr, err := NewClusterRole(MustAssetReader(ExternalDNSClusterRoleAsset))
	if err != nil {
		panic(err)
	}
	return cr
}

func ExternalDNSClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	crb, err := NewClusterRoleBinding(MustAssetReader(ExternalDNSClusterRoleBindingAsset))
	if err != nil {
		panic(err)
	}
	return crb
}

func ExternalDNSDeployment() *appsv1.Deployment {
	deploy, err := NewDeployment(MustAssetReader(ExternalDNSDeploymentAsset))
	if err != nil {
		panic(err)
	}
	return deploy
}

func NewServiceAccount(manifest io.Reader) (*corev1.ServiceAccount, error) {
	sa := corev1.ServiceAccount{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&sa); err != nil {
		return nil, err
	}
	return &sa, nil
}

func NewClusterRole(manifest io.Reader) (*rbacv1.ClusterRole, error) {
	cr := rbacv1.ClusterRole{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&cr); err != nil {
		return nil, err
	}
	return &cr, nil
}

func NewClusterRoleBinding(manifest io.Reader) (*rbacv1.ClusterRoleBinding, error) {
	crb := rbacv1.ClusterRoleBinding{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&crb); err != nil {
		return nil, err
	}
	return &crb, nil
}

func NewDeployment(manifest io.Reader) (*appsv1.Deployment, error) {
	deploy := appsv1.Deployment{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&deploy); err != nil {
		return nil, err
	}
	return &deploy, nil
}

func NewNamespace(manifest io.Reader) (*corev1.Namespace, error) {
	ns := corev1.Namespace{}
	if err := yaml.NewYAMLOrJSONDecoder(manifest, 100).Decode(&ns); err != nil {
		return nil, err
	}
	return &ns, nil
}
