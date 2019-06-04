package operator

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/resourcegroupstaggingapi"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
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
	kerrors "k8s.io/apimachinery/pkg/util/errors"
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
	kclient   client.Client
	dnsConfig *configv1.DNS
	provider  operatorv1.ProviderType
	tClient *resourcegroupstaggingapi.ResourceGroupsTaggingAPI
}

// New creates (but does not start) a new operator from configuration.
func New(config operatorconfig.Config, kubeConfig *rest.Config, dnsConfig *configv1.DNS) (*Operator, error) {
	kubeClient, err := operatorclient.NewClient(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube kclient: %v", err)
	}

	creds := credentials.NewStaticCredentials(string(config.Credentials.Data["aws_access_key_id"]), string(config.Credentials.Data["aws_secret_access_key"]), "")
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Credentials: creds,
		},
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create AWS client session: %v", err)
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
		Credentials:      config.Credentials,
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
		kclient:     kubeClient,
		namespace:   config.Namespace,
		dnsConfig:   dnsConfig,
		provider:    config.Provider,
		tClient: resourcegroupstaggingapi.New(sess, aws.NewConfig().WithRegion("us-east-1")),
	}, nil
}

// Start creates the default ExternalDNS and then starts the operator
// synchronously until a message is received on the stop channel.
// TODO: Move the default ExternalDNS logic elsewhere.
func (o *Operator) Start(stop <-chan struct{}) error {
	// Periodically ensure the default externaldns controller exists.
	go wait.Until(func() {
		if err := o.ensureDefaultPrivateExternalDNS(); err != nil {
			logrus.Errorf("failed to ensure default private zone externaldns: %v", err)
		}
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
	svc := operatorv1.ServiceType
	zone := operatorv1.PrivateZoneType
	id, err := o.getZoneIDFromTags(o.dnsConfig.Spec.PrivateZone)
	if err != nil {
		logrus.Errorf("failed to get zone id from tags: %v", err)
	}
	// TODO: Remove after testing. Tags are used for private zones and is broken upstream:
	// https://github.com/kubernetes-incubator/external-dns/issues/1019
	private := configv1.DNSZone{ID: id}
	edns := &operatorv1.ExternalDNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultExternalDNSPrivateZoneController,
			Namespace: o.namespace,
		},
		Spec: operatorv1.ExternalDNSSpec{
			Sources: []*operatorv1.SourceType{&svc},
			ZoneType: &zone,
			Provider: operatorv1.ProviderSpec{
				ZoneFilter: []*configv1.DNSZone{&private},
			},
		},
	}
	if err := o.kclient.Get(context.TODO(), types.NamespacedName{Namespace: edns.Namespace, Name: edns.Name}, edns); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := o.kclient.Create(context.TODO(), edns); err != nil {
			return fmt.Errorf("failed to create externaldns default private zone: %v", err)
		}
		logrus.Infof("created externaldns default private zone: %s", edns.Name)
	}
	return nil
}

// ensureDefaultPublicExternalDNS creates the default public zone externaldns
// if it does not already exist.
func (o *Operator) ensureDefaultPublicExternalDNS() error {
	svc := operatorv1.ServiceType
	zone := operatorv1.PublicZoneType
	edns := &operatorv1.ExternalDNS{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorcontroller.DefaultExternalDNSPublicZoneController,
			Namespace: o.namespace,
		},
		Spec: operatorv1.ExternalDNSSpec{
			Sources: []*operatorv1.SourceType{&svc},
			ZoneType: &zone,
			Provider: operatorv1.ProviderSpec{
				ZoneFilter: []*configv1.DNSZone{o.dnsConfig.Spec.PublicZone},
			},
		},
	}
	if err := o.kclient.Get(context.TODO(), types.NamespacedName{Namespace: edns.Namespace, Name: edns.Name}, edns); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := o.kclient.Create(context.TODO(), edns); err != nil {
			return fmt.Errorf("failed to create externaldns default public zone: %v", err)
		}
		logrus.Infof("created externaldns default public zone: %s", edns.Name)
	}
	return nil
}

// getZoneIDFromTags finds the ID of a Route53 hosted zone from the given zoneConfig
// by using tags to search for the zone. Returns an error if the zone can't be found.
func (o *Operator) getZoneIDFromTags(zoneConfig *configv1.DNSZone) (string, error) {
	// Even though we use filters when getting resources, the resources are still
	// paginated as though no filter were applied.  If the desired resource is not
	// on the first page, then GetResources will not return it.  We need to use
	// GetResourcesPages and possibly go through one or more empty pages of
	// resources till we find a resource that gets through the filters.
	var id string
	var innerError error
	f := func(resp *resourcegroupstaggingapi.GetResourcesOutput, lastPage bool) (shouldContinue bool) {
		for _, zone := range resp.ResourceTagMappingList {
			zoneARN, err := arn.Parse(aws.StringValue(zone.ResourceARN))
			if err != nil {
				innerError = fmt.Errorf("failed to parse hostedzone ARN %q: %v", aws.StringValue(zone.ResourceARN), err)
				return false
			}
			elems := strings.Split(zoneARN.Resource, "/")
			if len(elems) != 2 || elems[0] != "hostedzone" {
				innerError = fmt.Errorf("got unexpected resource ARN: %v", zoneARN)
				return false
			}
			id = elems[1]
			return false
		}
		return true
	}
	tagFilters := []*resourcegroupstaggingapi.TagFilter{}
	for k, v := range zoneConfig.Tags {
		tagFilters = append(tagFilters, &resourcegroupstaggingapi.TagFilter{
			Key:    aws.String(k),
			Values: []*string{aws.String(v)},
		})
	}
	outerError := o.tClient.GetResourcesPages(&resourcegroupstaggingapi.GetResourcesInput{
		ResourceTypeFilters: []*string{aws.String("route53:hostedzone")},
		TagFilters:          tagFilters,
	}, f)
	if err := kerrors.NewAggregate([]error{innerError, outerError}); err != nil {
		return id, fmt.Errorf("failed to get tagged resources: %v", err)
	}
	logrus.Infof("found hosted zone id %q using tags %q", id, zoneConfig.Tags)

	return id, nil
}
