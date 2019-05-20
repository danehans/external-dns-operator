package operator

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	operatorv1 "github.com/danehans/api/operator/v1"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/danehans/external-dns-operator/pkg/manifests"
	operatorclient "github.com/danehans/external-dns-operator/pkg/operator/client"
	operatorconfig "github.com/danehans/external-dns-operator/pkg/operator/config"
	operatorcontroller "github.com/danehans/external-dns-operator/pkg/operator/controller"

	appsv1 "k8s.io/api/apps/v1"

	"k8s.io/client-go/rest"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Operator is the scaffolding for the externaldns operator. It sets up dependencies
// and defines the topology of the operator and its managed components, wiring
// them together.
type Operator struct {
	namespace string
	manager   manager.Manager
	caches    []cache.Cache
	client    client.Client
}

// New creates (but does not start) a new operator from configuration.
func New(config operatorconfig.Config, kubeConfig *rest.Config) (*Operator, error) {
	kubeClient, err := operatorclient.NewClient(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube client: %v", err)
	}

	scheme := operatorclient.GetScheme()
	operatorManager, err := manager.New(kubeConfig, manager.Options{
		Namespace: config.Namespace,
		Scheme:    scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create operator manager: %v", err)
	}

	// Create and register the operator controller with the operator manager.
	operatorController, err := operatorcontroller.New(operatorManager, operatorcontroller.Config{
		KubeConfig:       kubeConfig,
		Namespace:        config.Namespace,
		ExternalDNSImage: config.ExternalDNSImage,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create operator controller: %v", err)
	}

	// Create additional controller event sources from informers in the managed
	// namespace. Any new managed resources outside the operator's namespace
	// should be added here.
	mapper, err := apiutil.NewDiscoveryRESTMapper(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get API Group-Resources")
	}
	operandCache, err := cache.New(kubeConfig, cache.Options{Namespace: "openshift-externaldns", Scheme: scheme, Mapper: mapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create openshift-externaldns cache: %v", err)
	}
	// Any types added to the list here will only queue an externaldns if the
	// resource has the expected label.
	for _, o := range []runtime.Object{
		&appsv1.Deployment{},
	} {
		// TODO: It may not be necessary to copy, but erring on the side of caution for
		//       now given we're in a loop.
		obj := o.DeepCopyObject()
		informer, err := operandCache.GetInformer(obj)
		if err != nil {
			return nil, fmt.Errorf("failed to get informer for %v: %v", obj, err)
		}
		err = operatorController.Watch(&source.Informer{Informer: informer}, &handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(func(a handler.MapObject) []reconcile.Request {
				labels := a.Meta.GetLabels()
				if extdnsName, ok := labels[manifests.OwningExternalDNSLabel]; ok {
					logrus.Infof("queueing externaldns: %s %s", extdnsName, a.Meta.GetSelfLink())
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Namespace: config.Namespace,
								Name:      extdnsName,
							},
						},
					}
				} else {
					return []reconcile.Request{}
				}
			}),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create watch for %v: %v", obj, err)
		}
	}

	return &Operator{
		manager: operatorManager,
		caches:  []cache.Cache{operandCache},

		// TODO: These are only needed for the default ingress controller stuff, which
		// should be refactored away.
		client:    kubeClient,
		namespace: config.Namespace,
	}, nil
}

// Start creates the default ExternalDNS and then starts the operator
// synchronously until a message is received on the stop channel.
// TODO: Move the default ExternalDNS logic elsewhere.
func (o *Operator) Start(stop <-chan struct{}) error {
	// Periodically ensure the default externaldns controller exists.
	go wait.Until(func() {
		//if err := o.ensureDefaultPrivateExternalDNS(); err != nil {
		//	logrus.Errorf("failed to ensure default private zone externaldns: %v", err)
		//}
		if err := o.ensureDefaultPublicExternalDNS(); err != nil {
			logrus.Errorf("failed to ensure default public zone externaldns: %v", err)
		}
	}, 1*time.Minute, stop)

	errChan := make(chan error)

	// Start the manager.
	go func() {
		errChan <- o.manager.Start(stop)
	}()

	// Wait for the manager to exit or a stop signal.
	select {
	case <-stop:
		return nil
	case err := <-errChan:
		return err
	}
}

// ensureDefaultPrivateExternalDNS creates the default private zone externaldns
// if it does not already exist.
func (o *Operator) ensureDefaultPrivateExternalDNS() error {
	private := &operatorv1.ExternalDNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultExternalDNSPrivateZoneController,
			Namespace: o.namespace,
		},
	}
	if err := o.client.Get(context.TODO(), types.NamespacedName{Namespace: private.Namespace, Name: private.Name}, private); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := o.client.Create(context.TODO(), private); err != nil {
			return fmt.Errorf("failed to create default private zone externaldns: %v", err)
		}
		logrus.Infof("created default private zone externaldns: %s", private.Name)
	}
	return nil
}

// ensureDefaultPublicExternalDNS creates the default public zone externaldns
// if it does not already exist.
func (o *Operator) ensureDefaultPublicExternalDNS() error {
	svc := operatorv1.ServiceType
	pubZone := operatorv1.PublicZoneType
	aws := operatorv1.AWSProvider
	pubFilter := configv1.DNSZone{
		ID: "Z3URY6TWQ91KVV",
	}
	pubDNS := &operatorv1.ExternalDNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultExternalDNSPublicZoneController,
			Namespace: o.namespace,
		},
		Spec: operatorv1.ExternalDNSSpec{
			BaseDomain: "dhansen.devcluster.openshift.com",
			Sources: []*operatorv1.SourceType{&svc},
			ZoneType: &pubZone,
			Provider: operatorv1.ProviderSpec{
				Type: &aws,
				ZoneFilter: []*configv1.DNSZone{&pubFilter},
				Args: []string{"--aws-zone-type=public", "--aws-api-retries=3"},
			},
		},
	}
	if err := o.client.Get(context.TODO(), types.NamespacedName{Namespace: pubDNS.Namespace, Name: pubDNS.Name}, pubDNS); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := o.client.Create(context.TODO(), pubDNS); err != nil {
			return fmt.Errorf("failed to create default public zone externaldns: %v", err)
		}
		logrus.Infof("created default public zone externaldns: %s", pubDNS.Name)
	}
	return nil
}
