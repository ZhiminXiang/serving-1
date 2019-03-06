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

package clusteringress

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/knative/pkg/tracker"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/knative/pkg/apis/istio/v1alpha3"
	istioinformers "github.com/knative/pkg/client/informers/externalversions/istio/v1alpha3"
	istiolisters "github.com/knative/pkg/client/listers/istio/v1alpha3"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/network"
	"github.com/knative/serving/pkg/reconciler"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/resources"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "clusteringress-controller"
)

// clusterIngressFinalizer is the name that we put into the resource finalizer list, e.g.
//  metadata:
//    finalizers:
//    - clusteringresses.networking.internal.knative.dev
var (
	//
	clusterIngressResource  = v1alpha1.Resource("clusteringresses")
	clusterIngressFinalizer = clusterIngressResource.String()
)

type configStore interface {
	ToContext(ctx context.Context) context.Context
	WatchConfigs(w configmap.Watcher)
}

// Reconciler implements controller.Reconciler for ClusterIngress resources.
type Reconciler struct {
	// The Mutex is needed to guard Istio Gateway because Istio Gateway is
	// a global resource that should be modified by a single thread.
	mutex sync.Mutex

	*reconciler.Base

	// listers index properties about resources
	clusterIngressLister listers.ClusterIngressLister
	virtualServiceLister istiolisters.VirtualServiceLister
	gatewayLister        istiolisters.GatewayLister
	secretLister         corev1listers.SecretLister
	configStore          configStore

	tracker tracker.Interface

	enableReconcilingGateway bool
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	clusterIngressInformer informers.ClusterIngressInformer,
	virtualServiceInformer istioinformers.VirtualServiceInformer,
	gatewayInformer istioinformers.GatewayInformer,
	secretInfomer corev1informers.SecretInformer,
) *controller.Impl {

	c := &Reconciler{
		Base:                     reconciler.NewBase(opt, controllerAgentName),
		clusterIngressLister:     clusterIngressInformer.Lister(),
		virtualServiceLister:     virtualServiceInformer.Lister(),
		gatewayLister:            gatewayInformer.Lister(),
		secretLister:             secretInfomer.Lister(),
		enableReconcilingGateway: false,
	}
	impl := controller.NewImpl(c, c.Logger, "ClusterIngresses", reconciler.MustNewStatsReporter("ClusterIngress", c.Logger))

	c.Logger.Info("Setting up event handlers")
	myFilterFunc := reconciler.AnnotationFilterFunc(networking.IngressClassAnnotationKey, network.IstioIngressClassName, true)
	clusterIngressInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.Enqueue,
			UpdateFunc: controller.PassNew(impl.Enqueue),
			DeleteFunc: impl.Enqueue,
		},
	})

	virtualServiceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: myFilterFunc,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    impl.EnqueueLabelOfClusterScopedResource(networking.IngressLabelKey),
			UpdateFunc: controller.PassNew(impl.EnqueueLabelOfClusterScopedResource(networking.IngressLabelKey)),
			DeleteFunc: impl.EnqueueLabelOfClusterScopedResource(networking.IngressLabelKey),
		},
	})

	c.tracker = tracker.NewNonExpiration(impl.EnqueueKey)
	gvk := corev1.SchemeGroupVersion.WithKind("Secret")
	secretInfomer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
		UpdateFunc: controller.PassNew(controller.EnsureTypeMeta(c.tracker.OnChanged, gvk)),
		DeleteFunc: controller.EnsureTypeMeta(c.tracker.OnChanged, gvk),
	})

	c.Logger.Info("Setting up ConfigMap receivers")
	resyncIngressesOnIstioConfigChange := configmap.TypeFilter(&config.Istio{})(func(string, interface{}) {
		impl.GlobalResync(clusterIngressInformer.Informer())
	})
	c.configStore = config.NewStore(c.Logger.Named("config-store"), resyncIngressesOnIstioConfigChange)
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)
	return impl
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ClusterIngress resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	ctx = c.configStore.ToContext(ctx)

	// Get the ClusterIngress resource with this name.
	original, err := c.clusterIngressLister.Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("clusteringress %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	// Don't modify the informers copy
	ci := original.DeepCopy()

	// Reconcile this copy of the ClusterIngress and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, ci)
	if equality.Semantic.DeepEqual(original.Status, ci.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(ci); err != nil {
		logger.Warn("Failed to update clusterIngress status", zap.Error(err))
		c.Recorder.Eventf(ci, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for ClusterIngress %q: %v", ci.Name, err)
		return err
	}
	return err
}

// Update the Status of the ClusterIngress.  Caller is responsible for checking
// for semantic differences before calling.
func (c *Reconciler) updateStatus(desired *v1alpha1.ClusterIngress) (*v1alpha1.ClusterIngress, error) {
	ci, err := c.clusterIngressLister.Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(ci.Status, desired.Status) {
		return ci, nil
	}
	// Don't modify the informers copy
	existing := ci.DeepCopy()
	existing.Status = desired.Status
	return c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().UpdateStatus(existing)
}

func (c *Reconciler) reconcile(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)
	if ci.GetDeletionTimestamp() != nil {
		return c.reconcileDeletion(ctx, ci)
	}

	// We may be reading a version of the object that was stored at an older version
	// and may not have had all of the assumed defaults specified.  This won't result
	// in this getting written back to the API Server, but lets downstream logic make
	// assumptions about defaulting.
	ci.SetDefaults()

	ci.Status.InitializeConditions()
	gatewayNames := gatewayNamesFromContext(ctx, ci)
	vs := resources.MakeVirtualService(ci, gatewayNames)

	logger.Infof("Reconciling clusterIngress :%v", ci)
	logger.Info("Creating/Updating VirtualService")
	if err := c.reconcileVirtualService(ctx, ci, vs); err != nil {
		// TODO(lichuqiang): should we explicitly mark the ingress as unready
		// when error reconciling VirtualService?
		return err
	}

	// As underlying network programming (VirtualService now) is stateless,
	// here we simply mark the ingress as ready if the VirtualService
	// is successfully synced.
	ci.Status.MarkNetworkConfigured()
	ci.Status.MarkLoadBalancerReady(getLBStatus(gatewayServiceURLFromContext(ctx, ci)))
	ci.Status.ObservedGeneration = ci.Generation

	// TODO(zhiminx): currently we turn off Gateway reconciliation as it relies
	// on Istio 1.1, which is not ready.
	// We should eventually use a feature flag (in ConfigMap) to turn this on/off.

	if c.enableReconcilingGateway {
		// Add the finalizer before adding `Servers` into Gateway so that we can be sure
		// the `Servers` get cleaned up from Gateway.
		if err := c.ensureFinalizer(ci); err != nil {
			return err
		}

		if err := c.reconcileCertSecrets(ctx, ci); err != nil {
			return err
		}

		if err := c.reconcileGateways(ctx, ci, gatewayNames, false); err != nil {
			return err
		}
	}

	// TODO(zhiminx): Mark Route status to indicate that Gateway is configured.

	logger.Info("ClusterIngress successfully synced")
	return nil
}

func getLBStatus(gatewayServiceURL string) []v1alpha1.LoadBalancerIngressStatus {
	// The ClusterIngress isn't load-balanced by any particular
	// Service, but through a Service mesh.
	if gatewayServiceURL == "" {
		return []v1alpha1.LoadBalancerIngressStatus{
			{MeshOnly: true},
		}
	}
	return []v1alpha1.LoadBalancerIngressStatus{
		{DomainInternal: gatewayServiceURL},
	}
}

// gatewayServiceURLFromContext return an address of a load-balancer
// that the given ClusterIngress is exposed to, or empty string if
// none.
func gatewayServiceURLFromContext(ctx context.Context, ci *v1alpha1.ClusterIngress) string {
	cfg := config.FromContext(ctx).Istio
	if len(cfg.IngressGateways) > 0 && ci.IsPublic() {
		return cfg.IngressGateways[0].ServiceURL
	}
	if len(cfg.LocalGateways) > 0 && !ci.IsPublic() {
		return cfg.LocalGateways[0].ServiceURL
	}
	return ""
}

func gatewayNamesFromContext(ctx context.Context, ci *v1alpha1.ClusterIngress) []string {
	gateways := []string{}
	if ci.IsPublic() {
		for _, gw := range config.FromContext(ctx).Istio.IngressGateways {
			gateways = append(gateways, gw.GatewayName)
		}
	} else {
		for _, gw := range config.FromContext(ctx).Istio.LocalGateways {
			gateways = append(gateways, gw.GatewayName)
		}
	}
	return dedup(gateways)
}

func dedup(strs []string) []string {
	existed := sets.NewString()
	unique := []string{}
	// We can't just do `sets.NewString(str)`, since we need to preserve the order.
	for _, s := range strs {
		if !existed.Has(s) {
			existed.Insert(s)
			unique = append(unique, s)
		}
	}
	return unique
}

func (c *Reconciler) reconcileVirtualService(ctx context.Context, ci *v1alpha1.ClusterIngress,
	desired *v1alpha3.VirtualService) error {
	logger := logging.FromContext(ctx)
	ns := desired.Namespace
	name := desired.Name

	vs, err := c.virtualServiceLister.VirtualServices(ns).Get(name)
	if apierrs.IsNotFound(err) {
		vs, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Create(desired)
		if err != nil {
			logger.Errorw("Failed to create VirtualService", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create VirtualService %q/%q: %v", ns, name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Created",
			"Created VirtualService %q", desired.Name)
	} else if err != nil {
		return err
	} else if !metav1.IsControlledBy(vs, ci) {
		// Surface an error in the ClusterIngress's status, and return an error.
		ci.Status.MarkResourceNotOwned("VirtualService", name)
		return fmt.Errorf("ClusterIngress: %q does not own VirtualService: %q", ci.Name, name)
	} else if !equality.Semantic.DeepEqual(vs.Spec, desired.Spec) {
		// Don't modify the informers copy
		existing := vs.DeepCopy()
		existing.Spec = desired.Spec
		_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update VirtualService", zap.Error(err))
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
			"Updated status for VirtualService %q/%q", ns, name)
	}

	return nil
}

func (c *Reconciler) ensureFinalizer(ci *v1alpha1.ClusterIngress) error {
	finalizers := sets.NewString(ci.Finalizers...)
	if finalizers.Has(clusterIngressFinalizer) {
		return nil
	}
	finalizers.Insert(clusterIngressFinalizer)

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      finalizers.List(),
			"resourceVersion": ci.ResourceVersion,
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Patch(ci.Name, types.MergePatchType, patch)
	return err
}

func (c *Reconciler) reconcileDeletion(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	logger := logging.FromContext(ctx)

	// If our Finalizer is first, delete the `Servers` from Gateway for this ClusterIngress,
	// and remove the finalizer.
	if len(ci.Finalizers) == 0 || ci.Finalizers[0] != clusterIngressFinalizer {
		return nil
	}

	gatewayNames := gatewayNamesFromContext(ctx, ci)
	// Delete the Servers from Gateway for this ClusterIngress.
	logger.Info("Cleaning up Gateway Servers")
	if err := c.reconcileGateways(ctx, ci, gatewayNames, true); err != nil {
		return err
	}

	// As ClusterIngress is being deleted, the ClusterIngress does not need to track any secrets.
	for _, tls := range ci.Spec.TLS {
		gvk := corev1.SchemeGroupVersion.WithKind("Secret")
		c.tracker.UnTrack(objectRefFromKey(fmt.Sprintf("%s/%s", tls.SecretNamespace, tls.SecretName), gvk), ci)
	}

	// Update the Route to remove the Finalizer.
	logger.Info("Removing Finalizer")
	ci.Finalizers = ci.Finalizers[1:]
	_, err := c.ServingClientSet.NetworkingV1alpha1().ClusterIngresses().Update(ci)
	return err
}

func (c *Reconciler) reconcileGateways(ctx context.Context, ci *v1alpha1.ClusterIngress, gatewayNames []string, deletesClusterIngress bool) error {
	for _, gatewayName := range gatewayNames {
		if err := c.reconcileGateway(ctx, ci, gatewayName, deletesClusterIngress); err != nil {
			return err
		}
	}
	return nil
}

func (c *Reconciler) reconcileGateway(ctx context.Context, ci *v1alpha1.ClusterIngress, gatewayName string, deletesClusterIngress bool) error {
	// We need to hold a lock to do read-modify-write operation for Istio Gateway because it is a
	// global resource across all of ClusterIngresses. So the Istio Gateway should only be updated
	// by a single thread.
	c.mutex.Lock()
	defer c.mutex.Unlock()
	logger := logging.FromContext(ctx)
	gateway, err := c.gatewayLister.Gateways(system.Namespace()).Get(gatewayName)
	if err != nil {
		// Not like VirtualService, A default gateway needs to be existed.
		// It should be installed when installing Knative.
		logger.Errorw("Failed to get Gateway.", zap.Error(err))
		return err
	}

	existing := resources.GetServers(gateway, ci)
	want := []v1alpha3.Server{}
	if !deletesClusterIngress {
		want = resources.MakeServers(ci)
	}

	if equality.Semantic.DeepEqual(existing, want) {
		return nil
	}

	copy := gateway.DeepCopy()
	copy = resources.UpdateGateway(copy, want, existing)
	if _, err := c.SharedClientSet.NetworkingV1alpha3().Gateways(copy.Namespace).Update(copy); err != nil {
		logger.Errorw("Failed to update Gateway", zap.Error(err))
		return err
	}
	c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
		"Updated Gateway %q/%q", gateway.Namespace, gateway.Name)
	return nil
}

func (c *Reconciler) reconcileCertSecrets(ctx context.Context, ci *v1alpha1.ClusterIngress) error {
	inusedSecretKeys := resources.GetOriginSecrets(ci)
	gatewaySvcNamespaces := getGatewaySvcNamespaces(ctx)
	for _, ns := range gatewaySvcNamespaces {
		if err := c.cleanUpUnsedSecret(ctx, ci, ns, inusedSecretKeys); err != nil {
			return err
		}
		for _, tls := range ci.Spec.TLS {
			if err := c.reconcileCertSecret(ctx, ci, &tls, ns); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Reconciler) reconcileCertSecret(ctx context.Context, ci *v1alpha1.ClusterIngress, tls *v1alpha1.ClusterIngressTLS, gatewayNamespace string) error {
	logger := logging.FromContext(ctx)
	originSecret, err := c.secretLister.Secrets(tls.SecretNamespace).Get(tls.SecretName)
	if err != nil {
		return err
	}

	gvk := corev1.SchemeGroupVersion.WithKind("Secret")
	c.tracker.Track(objectRef(originSecret, gvk), ci)

	desiredSecret := resources.MakeTargetSecret(originSecret, ci, "istio-system")
	existingSecret, err := c.secretLister.Secrets(desiredSecret.Namespace).Get(desiredSecret.Name)
	if apierrs.IsNotFound(err) {
		_, err = c.KubeClientSet.CoreV1().Secrets(desiredSecret.Namespace).Create(desiredSecret)
		if err != nil {
			logger.Errorw("Failed to create Certificate Secret", zap.Error(err))
			c.Recorder.Eventf(ci, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Secret %q/%q: %v", desiredSecret.Namespace, desiredSecret.Name, err)
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Created",
			"Created Secret %q/%q", desiredSecret.Namespace, desiredSecret.Name)
	} else if err != nil {
		return err
	} else if !equality.Semantic.DeepEqual(existingSecret.Data, desiredSecret.Data) {
		// Don't modify the informers copy
		existing := existingSecret.DeepCopy()
		existing.Data = desiredSecret.Data
		//_, err = c.SharedClientSet.NetworkingV1alpha3().VirtualServices(ns).Update(existing)
		_, err = c.KubeClientSet.CoreV1().Secrets(existing.Namespace).Update(existing)
		if err != nil {
			logger.Errorw("Failed to update target secret", zap.Error(err))
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Updated",
			"Updated status for Secret %q/%q", existing.Namespace, existing.Name)
	}
	return nil
}

func (c *Reconciler) cleanUpUnsedSecret(ctx context.Context, ci *v1alpha1.ClusterIngress, gatewayNamespace string, inuseSecrets sets.String) error {
	secrets, err := c.secretLister.Secrets(gatewayNamespace).List(resources.MakeSecretSelector(ci))
	if apierrs.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	// We make copy of secrets to avoid modifying the informers copy.
	copySecrets := resources.CopySecrets(secrets)
	for _, secret := range copySecrets {
		originSecretKey := fmt.Sprintf("%s/%s", secret.Labels[networking.OriginSecretNamespaceLabelKey], secret.Labels[networking.OriginSecretNameLabelKey])
		if inuseSecrets.Has(originSecretKey) {
			continue
		}

		gvk := corev1.SchemeGroupVersion.WithKind("Secret")
		c.tracker.UnTrack(objectRefFromKey(originSecretKey, gvk), ci)

		if err := c.KubeClientSet.CoreV1().Secrets(secret.Namespace).Delete(secret.Name, &metav1.DeleteOptions{}); err != nil {
			return err
		}
		c.Recorder.Eventf(ci, corev1.EventTypeNormal, "Deleted",
			"Deleted Secret %q/%q", secret.Namespace, secret.Name)
	}
	return nil
}

func getGatewaySvcNamespaces(ctx context.Context) []string {
	cfg := config.FromContext(ctx).Istio
	namespaces := []string{}
	for _, ingressgateway := range cfg.IngressGateways {
		ns := strings.Split(ingressgateway.ServiceURL, ".")[1]
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

/////////////////////////////////////////
// Misc helpers.
/////////////////////////////////////////

// TODO(zhiminx): move this part into a shareable directory.
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

func objectRefFromKey(key string, gvk schema.GroupVersionKind) corev1.ObjectReference {
	apiVersion, kind := gvk.ToAPIVersionAndKind()
	str := strings.Split(key, "/")
	return corev1.ObjectReference{
		APIVersion: apiVersion,
		Kind:       kind,
		Namespace:  str[0],
		Name:       str[1],
	}
}
