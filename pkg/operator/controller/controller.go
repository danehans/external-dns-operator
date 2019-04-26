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

		// For now, if the cluster configs are unavailable, defer reconciliation
		// because weaving conditionals everywhere to deal with various nil states
		// is too complicated. It doesn't seem too risky to rely on the invariant
		// of the cluster config being available.
		if dnsConfig != nil && infraConfig != nil {
			// Ensure we have all the necessary scaffolding on which to place externaldns instances.
			if err := r.ensureExternalDNSNamespace(edns); err != nil {
				errs = append(errs, fmt.Errorf("failed to ensure externaldns namespace: %v", err))
			}

			if edns.DeletionTimestamp != nil {
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

	// TODO: Should this be another controller?
	//if err := r.syncOperatorStatus(); err != nil {
	//	errs = append(errs, fmt.Errorf("failed to sync operator status: %v", err))
	//}

	// Log in case of errors as the controller's logs get eaten.
	if len(errs) > 0 {
		logrus.Errorf("failed to reconcile request %s: %v", request, utilerrors.NewAggregate(errs))
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
