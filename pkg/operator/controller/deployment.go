package controller

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	operatorv1 "github.com/danehans/api/operator/v1"
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
func (r *reconciler) ensureExternalDNSDeployment(eds *operatorv1.ExternalDNS, dnsConfig *configv1.DNS,
	infraConfig *configv1.Infrastructure) error {
	desired := r.desiredExternalDNSDeployment(eds, r.Config.ExternalDNSImage, infraConfig)
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
func (r *reconciler) ensureExternalDNSDeploymentDeleted(eds *operatorv1.ExternalDNS) error {
	deployment := &appsv1.Deployment{}
	name := ExternalDNSDeploymentNamespacedName(eds)
	deployment.Name = name.Name
	deployment.Namespace = name.Namespace
	if err := r.kclient.Delete(context.TODO(), deployment); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// desiredExternalDNSDeployment returns the desired ExternalDNS deployment.
func (r *reconciler) desiredExternalDNSDeployment(edns *operatorv1.ExternalDNS, ExternalDNSImage string,
	infraConfig *configv1.Infrastructure) *appsv1.Deployment {
	deployment := manifests.ExternalDNSDeployment()
	name := ExternalDNSDeploymentNamespacedName(edns)
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
									Values:   []string{ExternalDNSName(edns)},
								},
							},
						},
					},
				},
			},
		},
	}

	deployment.Spec.Template.Spec.Containers[0].Image = ExternalDNSImage

	owner := "--txt-owner-id=" + TextOwnerID(infraConfig, edns)
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
		"--registry=txt", owner)

	provider := "--provider=" + string(*edns.Status.ProviderType)
	deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, provider)

	//domain := "--domain-filter=" + strings.Trimedns.Status.BaseDomain
	//deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, domain)

	src := "--source="
	for _, s := range edns.Spec.Sources {
		src += string(*s)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, src)
	}

	if *edns.Status.ProviderType == operatorv1.AWSProvider {
		authEnvVars := []corev1.EnvVar{
			{
				Name: "AWS_ACCESS_KEY_ID",
				Value: string(r.Credentials.Data["aws_access_key_id"]),
			},
			{
				Name: "AWS_SECRET_ACCESS_KEY",
				Value: string(r.Credentials.Data["aws_secret_access_key"]),
			},
		}
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, authEnvVars...)
		deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
			"--no-aws-evaluate-target-health", "--aws-api-retries=3")
		if *edns.Spec.ZoneType == operatorv1.PublicZoneType {
			deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
				"--aws-zone-type=public")
		}
		if *edns.Spec.ZoneType == operatorv1.PrivateZoneType {
			deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args,
				"--aws-zone-type=private")
		}
	}

	if edns.Spec.Provider.Args != nil {
		for _, a := range edns.Spec.Provider.Args {
			deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, a)
		}
	}

	if edns.Spec.Provider.ZoneFilter != nil {
		for _, z := range edns.Spec.Provider.ZoneFilter {
			if len(z.ID) != 0 {
				zf := "--zone-id-filter=" + z.ID
				deployment.Spec.Template.Spec.Containers[0].Args = append(deployment.Spec.Template.Spec.Containers[0].Args, zf)
			}
			// Tag filters are broken upstream:
			// https://github.com/kubernetes-incubator/external-dns/issues/1019
		}
	}

	return deployment
}

// currentExternalDNSDeployment returns the current ExternalDNS deployment.
func (r *reconciler) currentExternalDNSDeployment(edns *operatorv1.ExternalDNS) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	if err := r.kclient.Get(context.TODO(), ExternalDNSDeploymentNamespacedName(edns), deployment); err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return deployment, nil
}

// createExternalDNSDeployment creates a ExternalDNS deployment.
func (r *reconciler) createExternalDNSDeployment(deployment *appsv1.Deployment) error {
	if err := r.kclient.Create(context.TODO(), deployment); err != nil {
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

	if err := r.kclient.Update(context.TODO(), updated); err != nil {
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
