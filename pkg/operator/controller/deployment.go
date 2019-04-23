package controller

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	extdnsv1a1 "github.com/danehans/external-dns-operator/pkg/apis/externaldns/v1alpha1"
	"github.com/danehans/external-dns-operator/pkg/manifests"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
)

// ensureExternalDNSDeployment ensures an ExternalDNS deployment exists for the
// given externalDNS resource.
func (r *reconciler) ensureExternalDNSDeployment(eds *extdnsv1a1.ExternalDNS, dnsConfig *configv1.DNS,
	infraConfig *configv1.Infrastructure) error {
	desired := desiredExternalDNSDeployment(eds, r.Config.ExternalDNSImage, dnsConfig, infraConfig)
	current, err := r.currentExternalDNSDeployment(eds)
	if err != nil {
		return err
	}
	switch {
	case current == nil:
		if err := r.createExternalDNSDeployment(desired); err != nil {
			return err
		}
	case current != nil:
		if err := r.updateExternalDNSDeployment(current, desired); err != nil {
			return err
		}
	}
	return nil
}

// ensureExternalDNSDeploymentDeleted ensures that any Deployment
// resources associated with the externaldns are deleted.
func (r *reconciler) ensureExternalDNSDeploymentDeleted(eds *extdnsv1a1.ExternalDNS) error {
	deployment := &appsv1.Deployment{}
	name := ExternalDNSDeploymentName(eds)
	deployment.Name = name.Name
	deployment.Namespace = name.Namespace
	if err := r.client.Delete(context.TODO(), deployment); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// desiredExternalDNSDeployment returns the desired ExternalDNS deployment.
func desiredExternalDNSDeployment(edns *extdnsv1a1.ExternalDNS, ExternalDNSImage string,
	dnsConfig *configv1.DNS, infraConfig *configv1.Infrastructure) *appsv1.Deployment {
	deployment := manifests.ExternalDNSDeployment()
	name := ExternalDNSDeploymentName(edns)
	deployment.Name = name.Name
	deployment.Namespace = name.Namespace

	deployment.Labels = map[string]string{
		// associate the deployment with the externaldns
		manifests.OwningExternalDNSLabel: edns.Name,
	}

	// Ensure the deployment adopts only its own pods.
	deployment.Spec.Selector = ExternalDNSDeploymentPodSelector(edns)
	deployment.Spec.Template.Labels = deployment.Spec.Selector.MatchLabels

	// Prevent colocation of controller pods to enable simple horizontal scaling
	deployment.Spec.Template.Spec.Affinity = &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						TopologyKey: "kubernetes.io/hostname",
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      controllerDeploymentLabel,
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{ExternalDNSDeploymentLabel(edns)},
								},
							},
						},
					},
				},
			},
		},
	}

	deployment.Spec.Template.Spec.Containers[0].Image = ExternalDNSImage

	owner := "--txt-owner-id=" + ExternalDNSDeploymentLabel(edns)
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
		"--registry=txt", owner)

	platform := "--provider=" + infraConfig.Status.Platform
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
		string(platform))

	domain := "--domain-filter=" + dnsConfig.Spec.BaseDomain
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, domain)

	// TODO: Use zone filters instead of domain filters.
	//       Can/Should zoneid & domain filters be combined?
	//zone := dnsConfig.Spec.PublicZone.ID
	//deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, zone)

	src := "--source="
	if edns.Spec.Sources != nil {
		for _, s := range edns.Spec.Sources {
			src += string(*s)
			deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
				src)
		}
	} else {
		svc := src + string(extdnsv1a1.ServiceType)
		ing := src + string(extdnsv1a1.IngressType)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
			svc, ing)
	}

	if platform == configv1.AWSPlatformType {
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
			"--no-aws-evaluate-target-health")

	}

	return deployment
}

// currentExternalDNSDeployment returns the current ExternalDNS deployment.
func (r *reconciler) currentExternalDNSDeployment(edns *extdnsv1a1.ExternalDNS) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	if err := r.client.Get(context.TODO(), ExternalDNSDeploymentName(edns), deployment); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return deployment, nil
}

// createExternalDNSDeployment creates a ExternalDNS deployment.
func (r *reconciler) createExternalDNSDeployment(deployment *appsv1.Deployment) error {
	if err := r.client.Create(context.TODO(), deployment); err != nil {
		return fmt.Errorf("failed to create ExternalDNS deployment %s/%s: %v", deployment.Namespace, deployment.Name, err)
	}
	logrus.Infof("created ExternalDNS deployment %s/%s", deployment.Namespace, deployment.Name)
	return nil
}

// updateExternalDNSDeployment updates a ExternalDNS deployment.
func (r *reconciler) updateExternalDNSDeployment(current, desired *appsv1.Deployment) error {
	changed, updated := deploymentConfigChanged(current, desired)
	if !changed {
		return nil
	}

	if err := r.client.Update(context.TODO(), updated); err != nil {
		return fmt.Errorf("failed to update ExternalDNS deployment %s/%s: %v", updated.Namespace, updated.Name, err)
	}
	logrus.Infof("updated ExternalDNS deployment %s/%s", updated.Namespace, updated.Name)
	return nil
}

// deploymentConfigChanged checks if current config matches the expected config
// for the externaldns deployment and if not returns the updated config.
func deploymentConfigChanged(current, expected *appsv1.Deployment) (bool, *appsv1.Deployment) {
	if cmp.Equal(current.Spec.Template.Spec.Containers[0].Args, expected.Spec.Template.Spec.Containers[0].Args, cmpopts.EquateEmpty()) &&
		current.Spec.Template.Spec.Containers[0].Image == expected.Spec.Template.Spec.Containers[0].Image {
		return false, nil
	}

	updated := current.DeepCopy()
	updated.Spec.Template.Spec.Containers[0].Args = expected.Spec.Template.Spec.Containers[0].Args
	updated.Spec.Template.Spec.Containers[0].Image = expected.Spec.Template.Spec.Containers[0].Image
	return true, updated
}
