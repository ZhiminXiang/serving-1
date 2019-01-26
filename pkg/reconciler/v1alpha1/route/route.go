/*
Copyright 2018 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package route

import (
	"context"
	"fmt"
	"sort"

	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/tracker"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	networkinginformers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	networkinglisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources"
	resourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources/names"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "route-controller"
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// Reconciler implements controller.Reconciler for Route resources.
type Reconciler struct {
	*reconciler.Base

	// Listers index properties about resources
	routeLister          listers.RouteLister
	configurationLister  listers.ConfigurationLister
	revisionLister       listers.RevisionLister
	serviceLister        corev1listers.ServiceLister
	clusterIngressLister networkinglisters.ClusterIngressLister
	certificateLister    networkinglisters.CertificateLister
	configStore          configStore
	tracker              tracker.Interface

	clock system.Clock
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events
// config - client configuration for talking to the apiserver
// si - informer factory shared across all controllers for listening to events and indexing resource properties
// reconcileKey - function for mapping queue keys to resource names
func NewController(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	configInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	serviceInformer corev1informers.ServiceInformer,
	clusterIngressInformer networkinginformers.ClusterIngressInformer,
	certificateInformer networkinginformers.CertificateInformer,
) *controller.Impl {
	return NewControllerWithClock(opt, routeInformer, configInformer, revisionInformer,
		serviceInformer, clusterIngressInformer, certificateInformer, system.RealClock{})
}

func NewControllerWithClock(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	configInformer servinginformers.ConfigurationInformer,
	revisionInformer servinginformers.RevisionInformer,
	serviceInformer corev1informers.ServiceInformer,
	clusterIngressInformer networkinginformers.ClusterIngressInformer,
	certificateInformer networkinginformers.CertificateInformer,
	clock system.Clock,
) *controller.Impl {

	// No need to lock domainConfigMutex yet since the informers that can modify
	// domainConfig haven't started yet.
	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		routeLister:          routeInformer.Lister(),
		configurationLister:  configInformer.Lister(),
		revisionLister:       revisionInformer.Lister(),
		serviceLister:        serviceInformer.Lister(),
		clusterIngressLister: clusterIngressInformer.Lister(),
		certificateLister:    certificateInformer.Lister(),
		clock:                clock,
	}
	impl := controller.NewImpl(c, c.Logger, "Routes", reconciler.MustNewStatsReporter("Routes", c.Logger))

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueControllerOf,
			UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
			DeleteFunc: impl.EnqueueControllerOf,
		},
	})

	clusterIngressInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.Filter(v1alpha1.SchemeGroupVersion.WithKind("Route")),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueLabelOfNamespaceScopedResource(serving.RouteNamespaceLabelKey, serving.RouteLabelKey),
			UpdateFunc: controller.PassNew(impl.EnqueueLabelOfNamespaceScopedResource(serving.RouteNamespaceLabelKey, serving.RouteLabelKey)),
			DeleteFunc: impl.EnqueueLabelOfNamespaceScopedResource(serving.RouteNamespaceLabelKey, serving.RouteLabelKey),
		},
	})

	// TODO(zhiminx): add EventHandler for Certificate

	c.tracker = tracker.New(impl.EnqueueKey, opt.GetTrackerLease())
	gvk := v1alpha1.SchemeGroupVersion.WithKind("Configuration")
	configInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
		UpdateFunc: controller.PassNew(controller.EnsureTypeMeta(c.tracker.OnChanged, gvk)),
		DeleteFunc: controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
	})
	gvk = v1alpha1.SchemeGroupVersion.WithKind("Revision")
	revisionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
		UpdateFunc: controller.PassNew(controller.EnsureTypeMeta(c.tracker.OnChanged, gvk)),
		DeleteFunc: controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	resyncRoutesOnConfigDomainChange := configmap.TypeFilter(&config.Domain{})(func(string, interface{}) {
		impl.GlobalResync(routeInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resyncRoutesOnConfigDomainChange)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}

/////////////////////////////////////////
//  Event handlers
/////////////////////////////////////////

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Route resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	ctx = c.configStore.ToContext(ctx)

	// Get the Route resource with this namespace/name.
	original, err := c.routeLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("route %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy.
	route := original.DeepCopy()

	// Reconcile this copy of the route and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, route)
	if equality.Semantic.DeepEqual(original.Status, route.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(route); err != nil {
		logger.Warn("Failed to update route status", zap.Error(err))
		c.Recorder.Eventf(route, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Route %q: %v", route.Name, err)
		return err
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, r *v1alpha1.Route) error {
	logger := logging.FromContext(ctx)

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	r.SetDefaults()

	r.Status.InitializeConditions()

	logger.Infof("Reconciling route: %v", r)
	// Configure traffic based on the RouteSpec.
	traffic, err := c.configureTraffic(ctx, r)
	if traffic == nil || err != nil {
		// Traffic targets aren't ready, no need to configure child resources.
		return err
	}

	logger.Info("Updating targeted revisions.")
	// In all cases we will add annotations to the referred targets.  This is so that when they become
	// routable we can know (through a listener) and attempt traffic configuration again.
	if err := c.reconcileTargetRevisions(ctx, traffic, r); err != nil {
		return err
	}

	// TODO(zhiminx): check if there is any reusable certificate first.
	desiredCert := makeCertificate(ctx, r, traffic)
	if err := c.reconcileCert(ctx, r, desiredCert); err != nil {
		logger.Error("Failed to reconcile certificate.", zap.Error(err))
		return err
	}
	tls := makeClusterIngressTLS(desiredCert)

	// TODO(zhiminx): update the route status when certificate is ready.

	// Update the information that makes us Addressable.
	r.Status.Domain = routeDomain(ctx, r)
	r.Status.DomainInternal = resourcenames.K8sServiceFullname(r)
	r.Status.Address = &duckv1alpha1.Addressable{
		Hostname: resourcenames.K8sServiceFullname(r),
	}

	logger.Info("Creating ClusterIngress.")
	clusterIngress, err := c.reconcileClusterIngress(ctx, r, resources.MakeClusterIngress(r, traffic, tls))
	if err != nil {
		return err
	}
	r.Status.PropagateClusterIngressStatus(clusterIngress.Status)

	logger.Info("Creating/Updating placeholder k8s services")
	if err := c.reconcilePlaceholderService(ctx, r, clusterIngress); err != nil {
		return err
	}

	logger.Info("Route successfully synced")
	return nil
}

// configureTraffic attempts to configure traffic based on the RouteSpec.  If there are missing
// targets (e.g. Configurations without a Ready Revision, or Revision that isn't Ready or Inactive),
// no traffic will be configured.
//
// If traffic is configured we update the RouteStatus with AllTrafficAssigned = True.  Otherwise we
// mark AllTrafficAssigned = False, with a message referring to one of the missing target.
func (c *Reconciler) configureTraffic(ctx context.Context, r *v1alpha1.Route) (*traffic.Config, error) {
	logger := logging.FromContext(ctx)
	t, err := traffic.BuildTrafficConfiguration(c.configurationLister, c.revisionLister, r)

	if t != nil {
		// Tell our trackers to reconcile Route whenever the things referred to by our
		// Traffic stanza change.
		gvk := v1alpha1.SchemeGroupVersion.WithKind("Configuration")
		for _, configuration := range t.Configurations {
			if err := c.tracker.Track(objectRef(configuration, gvk), r); err != nil {
				return nil, err
			}
		}
		gvk = v1alpha1.SchemeGroupVersion.WithKind("Revision")
		for _, revision := range t.Revisions {
			if revision.Status.IsActivationRequired() {
				logger.Infof("Revision %s/%s is inactive", revision.Namespace, revision.Name)
			}
			if err := c.tracker.Track(objectRef(revision, gvk), r); err != nil {
				return nil, err
			}
		}
	}

	badTarget, isTargetError := err.(traffic.TargetError)
	if err != nil && !isTargetError {
		// An error that's not due to missing traffic target should
		// make us fail fast.
		r.Status.MarkUnknownTrafficError(err.Error())
		return nil, err
	}
	if badTarget != nil && isTargetError {
		badTarget.MarkBadTrafficTarget(&r.Status)

		// Traffic targets aren't ready, no need to configure Route.
		return nil, nil
	}

	logger.Info("All referred targets are routable, marking AllTrafficAssigned with traffic information.")
	r.Status.Traffic = t.GetRevisionTrafficTargets()
	r.Status.MarkTrafficAssigned()

	return t, nil
}

func (c *Reconciler) reconcileCert(ctx context.Context, r *v1alpha1.Route, desiredCert *networkingv1alpha1.Certificate) error {
	logger := logging.FromContext(ctx)
	cert, err := c.certificateLister.Certificates(desiredCert.Namespace).Get(desiredCert.Name)
	if apierrs.IsNotFound(err) {
		cert, err = c.ServingClientSet.NetworkingV1alpha1().Certificates(desiredCert.Namespace).Create(desiredCert)
		if err != nil {
			logger.Error("Failed to create Certificate", zap.Error(err))
			c.Recorder.Eventf(r, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Certificate for route %s/%s: %v", r.Namespace, r.Name, err)
			return err
		}
		c.Recorder.Eventf(r, corev1.EventTypeNormal, "Created",
			"Created Certificate %q/%q", cert.Name, cert.Namespace)
		return nil
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(cert.Spec, desiredCert.Spec) {
		origin := cert.DeepCopy()
		origin.Spec = desiredCert.Spec
		if _, err := c.ServingClientSet.NetworkingV1alpha1().Certificates(origin.Namespace).Update(origin); err != nil {
			return err
		}
		c.Recorder.Eventf(origin, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Certificate %q/%q", origin.Name, origin.Namespace)
		return nil
	}
	return nil
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////

type accessor interface {
	GroupVersionKind() schema.GroupVersionKind
	GetNamespace() string
	GetName() string
}

func objectRef(a accessor, gvk schema.GroupVersionKind) corev1.ObjectReference {
	// We can't always rely on the TypeMeta being populated.
	// See: https://github.com/knative/serving/issues/2372
	// Also: https://github.com/kubernetes/apiextensions-apiserver/issues/29
	// gvk := a.GroupVersionKind()
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  a.GetNamespace(),
		Name:       a.GetName(),
	}
}

func routeDomain(ctx context.Context, route *v1alpha1.Route) string {
	domainConfig := config.FromContext(ctx).Domain
	domain := domainConfig.LookupDomainForLabels(route.ObjectMeta.Labels)
	return fmt.Sprintf("%s.%s.%s", route.Name, route.Namespace, domain)
}

func makeCertificate(ctx context.Context, route *v1alpha1.Route, traffic *traffic.Config) *networkingv1alpha1.Certificate {
	var dnsNames []string
	routeDNSName := routeDomain(ctx, route)
	dnsNames = append(dnsNames, routeDNSName)
	for name := range traffic.Targets {
		if len(name) > 0 {
			dnsNames = append(dnsNames, fmt.Sprintf("%s.%s", name, routeDNSName))
		}
	}

	dnsNames = dedup(dnsNames)
	sort.Strings(dnsNames)
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", route.Name, route.Namespace),
			// TODO(zhiminx): make this configurable
			Namespace: "istio-system",
			Labels: map[string]string{
				serving.RouteLabelKey:          route.Name,
				serving.RouteNamespaceLabelKey: route.Namespace,
			},
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(route)},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: fmt.Sprintf("%s.%s", route.Name, route.Namespace),
		},
	}
}

func dedup(strs []string) []string {
	existed := make(map[string]struct{})
	unique := []string{}
	for _, s := range strs {
		if _, ok := existed[s]; !ok {
			existed[s] = struct{}{}
			unique = append(unique, s)
		}
	}
	return unique
}

func makeClusterIngressTLS(cert *networkingv1alpha1.Certificate) []networkingv1alpha1.ClusterIngressTLS {
	var tls []networkingv1alpha1.ClusterIngressTLS
	tls = append(tls, networkingv1alpha1.ClusterIngressTLS{
		Hosts:           cert.Spec.DNSNames,
		SecretName:      cert.Spec.SecretName,
		SecretNamespace: cert.Namespace,
	})
	return tls
}
