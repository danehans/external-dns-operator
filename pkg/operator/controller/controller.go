package controller

import (
	"context"
	"fmt"

	operatorv1 "github.com/danehans/api/operator/v1"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/danehans/external-dns-operator/pkg/manifests"
	operatorclient "github.com/danehans/external-dns-operator/pkg/operator/client"
	"github.com/danehans/external-dns-operator/pkg/util/slice"

	"k8s.io/client-go/rest"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	// DefaultExternalDNSController is the name of the default ExternalDNS instance.
	DefaultExternalDNSController = "default"

	// ExternalDNSControllerFinalizer is applied to an ExternalDNS before being considered
	// for processing. This ensures the operator has a chance to handle all states.
	ExternalDNSControllerFinalizer = "externaldns.operator.openshift.io/externaldns-controller"

	// Unknown release version
	UnknownReleaseVersionName = "unknown"
)

// New creates the operator controller from configuration. This is the
// controller that handles all the logic for implementing externaldns
// based on ExternalDNS resources.
//
// The controller will be pre-configured to watch for ExternalDNS resources.
func New(mgr manager.Manager, config Config) (controller.Controller, error) {
	kubeClient, err := operatorclient.NewClient(config.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %v", err)
	}

	reconciler := &reconciler{
		Config: config,
		client: kubeClient,
	}
	c, err := controller.New("operator-controller", mgr, controller.Options{Reconciler: reconciler})
	if err != nil {
		return nil, err
	}
	if err := c.Watch(&source.Kind{Type: &operatorv1.ExternalDNS{}}, &handler.EnqueueRequestForObject{}); err != nil {
		return nil, err
	}

	return c, nil
}

// Config holds all the things necessary for the controller to run.
type Config struct {
	KubeConfig       *rest.Config
	Namespace        string
	ExternalDNSImage string
}

// reconciler handles the actual externaldns reconciliation logic in response to
// events.
type reconciler struct {
	Config

	// client is the kube Client and it will refresh scheme/mapper fields if needed
	// to detect some resources like ServiceMonitor which could get registered after
	// the client creation.
	// Since this controller is running in single threaded mode,
	// we do not need to synchronize when changing rest scheme/mapper fields.
	client kclient.Client
}

// Reconcile expects request to refer to a dns and will do all the work
// to ensure the dns is in the desired state.
func (r *reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	errs := []error{}
	result := reconcile.Result{}

	logrus.Infof("reconciling request: %v", request)

	if request.NamespacedName.Name != DefaultExternalDNSController {
		// Return a nil error value to avoid re-triggering the event.
		logrus.Errorf("skipping unexpected externaldns %s", request.NamespacedName.Name)
		return result, nil
	}
	// Get the current externaldns state.
	edns := &operatorv1.ExternalDNS{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, edns); err != nil {
		if errors.IsNotFound(err) {
			// This means the externaldns was already deleted/finalized and there
			// are stale queue entries (or something edge triggering from a related
			// resource that got deleted async).
			logrus.Infof("externaldns not found; reconciliation will be skipped for request: %v", request)
		} else {
			errs = append(errs, fmt.Errorf("failed to get externaldns %s: %v", request, err))
		}
		edns = nil
	}

	if edns != nil {
		dnsConfig := &configv1.DNS{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, dnsConfig); err != nil {
			errs = append(errs, fmt.Errorf("failed to get dns.config 'cluster': %v", err))
			dnsConfig = nil
		}
		infraConfig := &configv1.Infrastructure{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, infraConfig); err != nil {
			errs = append(errs, fmt.Errorf("failed to get infrastructure 'cluster': %v", err))
			infraConfig = nil
		}
		if dnsConfig != nil && infraConfig != nil {
			// Ensure we have all the necessary scaffolding on which to place externaldns instances.
			if err := r.ensureExternalDNSNamespace(edns); err != nil {
				errs = append(errs, fmt.Errorf("failed to ensure externaldns namespace: %v", err))
			}
			if err := r.enforceEffectiveBaseDomain(edns); err != nil {
				errs = append(errs, fmt.Errorf("failed to enforce the effective externaldns baseDomain for %s: %v", edns.Name, err))
			} else if IsStatusBaseDomainSet(edns) {
				if err := r.enforceEffectiveProvider(edns, infraConfig); err != nil {
					errs = append(errs, fmt.Errorf("failed to enforce the effective provider for externaldns %s: %v", edns.Name, err))
				} else if edns.DeletionTimestamp != nil {
					// Handle deletion.
					if err := r.ensureExternalDNSDeleted(edns); err != nil {
						errs = append(errs, fmt.Errorf("failed to ensure deletion for externaldns %s: %v", edns.Name, err))
					}
				} else if err := r.enforceExternalDNSFinalizer(edns); err != nil {
					errs = append(errs, fmt.Errorf("failed to enforce finalizer for externaldns %s: %v", edns.Name, err))
				} else {
					// Handle everything else.
					if err := r.ensureExternalDNS(edns, dnsConfig, infraConfig); err != nil {
						errs = append(errs, fmt.Errorf("failed to ensure dns %s: %v", edns.Name, err))
					}
				}
			}
		}
	}

	// Log in case of errors as the controller's logs get eaten.
	if len(errs) > 0 {
		logrus.Errorf("failed to reconcile request %s: %v", request, utilerrors.NewAggregate(errs))
	} else {
		logrus.Infof("successfully reconciled request: %s", request)
	}
	return result, utilerrors.NewAggregate(errs)
}

// ensureExternalDNSNamespace ensures all the necessary scaffolding exists
// for externaldns generally, including a namespace and all RBAC setup.
func (r *reconciler) ensureExternalDNSNamespace(edns *operatorv1.ExternalDNS) error {
	ns := manifests.ExternalDNSNamespace()
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: ns.Name}, ns); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get externaldns namespace %q: %v", ns.Name, err)
		}
		if err := r.client.Create(context.TODO(), ns); err != nil {
			return fmt.Errorf("failed to create externaldns namespace %s: %v", ns.Name, err)
		}
		logrus.Infof("created externaldns namespace: %s", ns.Name)
	}

	cr := manifests.ExternalDNSClusterRole()
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: cr.Name}, cr); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get externaldns cluster role %s: %v", cr.Name, err)
		}
		if err := r.client.Create(context.TODO(), cr); err != nil {
			return fmt.Errorf("failed to create externaldns cluster role %s: %v", cr.Name, err)
		}
		logrus.Infof("created externaldns cluster role: %s", cr.Name)
	}

	crb := manifests.ExternalDNSClusterRoleBinding()
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: crb.Name}, crb); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get externaldns cluster role binding %s: %v", crb.Name, err)
		}
		if err := r.client.Create(context.TODO(), crb); err != nil {
			return fmt.Errorf("failed to create externaldns cluster role binding %s: %v", crb.Name, err)
		}
		logrus.Infof("created externaldns cluster role binding: %s", crb.Name)
	}

	sa := manifests.ExternalDNSServiceAccount()
	if err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: sa.Namespace, Name: sa.Name}, sa); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get externaldns service account %s/%s: %v", sa.Namespace, sa.Name, err)
		}
		if err := r.client.Create(context.TODO(), sa); err != nil {
			return fmt.Errorf("failed to create externaldns service account %s/%s: %v", sa.Namespace, sa.Name, err)
		}
		logrus.Infof("created externaldns service account: %s/%s", sa.Namespace, sa.Name)
	}

	return nil
}

// enforceEffectiveBaseDomain determines the effective baseDomain for the
// given externaldns and and publishes it to the externaldns's status.
func (r *reconciler) enforceEffectiveBaseDomain(edns *operatorv1.ExternalDNS) error {
	// An externaldns' baseDomain is immutable, so if has
	// been published to status, continue using it.
	if IsStatusBaseDomainSet(edns) {
		return nil
	}

	updated := edns.DeepCopy()
	var domain string
	switch {
	case len(edns.Spec.BaseDomain) > 0:
		domain = edns.Spec.BaseDomain
	default:
		domain = edns.Spec.BaseDomain
	}
	unique, err := r.isBaseDomainUnique(domain)
	if err != nil {
		return err
	}
	if !unique {
		logrus.Infof("baseDomain not unique, not setting ExternalDNS .status.baseDomain for %s/%s", edns.Namespace, edns.Name)
	} else {
		updated.Status.BaseDomain = domain
	}

	if err := r.client.Status().Update(context.TODO(), updated); err != nil {
		return fmt.Errorf("failed to update status of ExternalDNS %s/%s: %v", updated.Namespace, updated.Name, err)
	}

	return nil
}

// isBaseDomainUnique compares baseDomain with spec.domain of all
// externalDNSes and returns false if a conflict exists or an error
// if the externalDNS list operation returns an error.
func (r *reconciler) isBaseDomainUnique(domain string) (bool, error) {
	dnses := &operatorv1.ExternalDNSList{}
	if err := r.client.List(context.TODO(), dnses, kclient.InNamespace(r.Namespace)); err != nil {
		return false, fmt.Errorf("failed to list externaldnses: %v", err)
	}

	// Compare domain with all externaldnses for a conflict.
	for _, dns := range dnses.Items {
		if domain == dns.Status.BaseDomain {
			logrus.Infof("baseDomain %q conflicts with existing ExternalDNS: %s/%s", domain, dns.Namespace, dns.Name)
			return false, nil
		}
	}

	return true, nil
}

// IsStatusBaseDomainSet checks whether status.baseDomain of edns is set.
func IsStatusBaseDomainSet(edns *operatorv1.ExternalDNS) bool {
	if len(edns.Status.BaseDomain) == 0 {
		return false
	}
	return true
}

// providerTypeForInfra returns the appropriate provider
// type for the given infraConfig.
func providerTypeForInfra(infraConfig *configv1.Infrastructure) *operatorv1.ProviderType {
	var provider operatorv1.ProviderType

	switch infraConfig.Status.Platform {
	case configv1.AWSPlatformType:
		provider = operatorv1.AWSProvider
	case configv1.AzurePlatformType:
		provider = operatorv1.AzureProvider
	case configv1.GCPPlatformType:
		provider = operatorv1.GoogleProvider
	}

	return &provider
}

// enforceEffectiveProvider uses the infrastructure config to
// determine the appropriate provider configuration for the
// given externaldns and publishes it to the externaldns' status.
func (r *reconciler) enforceEffectiveProvider(edns *operatorv1.ExternalDNS, infraConfig *configv1.Infrastructure) error {
	// The externaldns' provider is immutable, so
	// if we have previously published a strategy in status, we must
	// continue to use that strategy it.
	if IsStatusProviderSet(edns) {
		return nil
	}

	updated := edns.DeepCopy()
	switch {
	case edns.Spec.Provider.Type != nil:
		updated.Status.ProviderType = edns.Spec.Provider.Type
	default:
		updated.Status.ProviderType = providerTypeForInfra(infraConfig)
	}
	if err := r.client.Status().Update(context.TODO(), updated); err != nil {
		return fmt.Errorf("failed to update status of externaldns %s/%s: %v", updated.Namespace, updated.Name, err)
	}

	return nil
}

// IsStatusProviderSet checks whether status.provider of edns is set.
func IsStatusProviderSet(edns *operatorv1.ExternalDNS) bool {
	if edns.Status.ProviderType != nil {
		return true
	}
	return false
}

// enforceExternalDNSFinalizer adds ExternalDNSControllerFinalizer to externaldns
// if it doesn't exist.
func (r *reconciler) enforceExternalDNSFinalizer(edns *operatorv1.ExternalDNS) error {
	if !slice.ContainsString(edns.Finalizers, ExternalDNSControllerFinalizer) {
		edns.Finalizers = append(edns.Finalizers, ExternalDNSControllerFinalizer)
		if err := r.client.Update(context.TODO(), edns); err != nil {
			return err
		}
		logrus.Infof("enforced finalizer for externaldns: %s", edns.Name)
	}
	return nil
}

// removeExternalDNSFinalizer removes ExternalDNSControllerFinalizer from externaldns
// if it exists.
func (r *reconciler) removeExternalDNSFinalizer(edns *operatorv1.ExternalDNS) error {
	if slice.ContainsString(edns.Finalizers, ExternalDNSControllerFinalizer) {
		updated := edns.DeepCopy()
		updated.Finalizers = slice.RemoveString(updated.Finalizers, ExternalDNSControllerFinalizer)
		if err := r.client.Update(context.TODO(), updated); err != nil {
			return err
		}
	}
	return nil
}

// ensureExternalDNSDeleted tries to delete externaldns dependent resources.
func (r *reconciler) ensureExternalDNSDeleted(edns *operatorv1.ExternalDNS) error {
	if err := r.ensureExternalDNSDeploymentDeleted(edns); err != nil {
		return fmt.Errorf("failed to delete deployment for externaldns %s: %v", edns.Name, err)
	}
	if err := r.removeExternalDNSFinalizer(edns); err != nil {
		return fmt.Errorf("failed to remove finalizer from externaldns %s: %v", edns.Name, err)

	}
	return nil
}

// ensureExternalDNS ensures all dependant externaldns resources exist
// for a given externaldns.
func (r *reconciler) ensureExternalDNS(edns *operatorv1.ExternalDNS, dnsConfig *configv1.DNS,
	infraConfig *configv1.Infrastructure) error {
	if err := r.ensureExternalDNSDeployment(edns, dnsConfig, infraConfig); err != nil {
		return fmt.Errorf("failed to ensure deployment for externaldns %s: %v", edns.Name, err)
	}
	return nil
}
