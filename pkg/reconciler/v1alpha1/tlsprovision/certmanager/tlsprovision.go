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

package tlsprovision

import (
	"context"
	"fmt"
	"strings"

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	certmanagerclientset "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions/certmanager/v1alpha1"
	certmanagerlisters "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1alpha1"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	routereconciler "github.com/knative/serving/pkg/reconciler/v1alpha1/route"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/config"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "tls-certmanager-controller"

type CertificateBuilder interface {
	Build(ctx context.Context, route *v1alpha1.Route, hosts []string, certName string) (*certmanagerv1alpha1.Certificate, error)
}

// Reconciler implements the logic of requesting certificate for new Route.
type Reconciler struct {
	*reconciler.Base
	routeLister          listers.RouteLister
	certificateLister    certmanagerlisters.CertificateLister
	certManagerClientSet certmanagerclientset.Interface
	configStore          routereconciler.ConfigStore
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	certificateInformer certmanagerinformers.CertificateInformer,
	certManagerClientSet certmanagerclientset.Interface,
) *controller.Impl {

	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		routeLister:          routeInformer.Lister(),
		certificateLister:    certificateInformer.Lister(),
		certManagerClientSet: certManagerClientSet,
	}

	impl := controller.NewImpl(c, c.Logger, "TLSProvisionCertManager")

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueWithCertManagerLabel(impl),
		UpdateFunc: controller.PassNew(c.enqueueWithCertManagerLabel(impl)),

		// TODO(zhiminx): consider how we handle the scenario of deleting Route.
	})

	certificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueCreatorRoute(impl),
		UpdateFunc: controller.PassNew(c.enqueueCreatorRoute(impl)),
	})

	// TODO(zhiminx): use the new version of NewStore.
	c.configStore = config.NewStore(c.Logger.Named("config-store"))
	c.configStore.WatchConfigs(opt.ConfigMapWatcher)

	return impl
}

// Reconcile attemps to converge below actual state and desired state by requesting certificates
// from cert-manager.
// Desired State: There are certificates provided for a given Route.
// Actual State: No certificate is provided.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	logger := logging.FromContext(ctx)

	ctx = c.configStore.ToContext(ctx)

	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	// Get the Route resource with this namespace/name
	route, err := c.routeLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("Route %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}
	return c.reconcileCertificates(ctx, route)
}

func (c *Reconciler) reconcileCertificates(ctx context.Context, route *v1alpha1.Route) error {

	logger := logging.FromContext(ctx)

	desiredCerts, err := c.buildDesiredCerts(ctx, route)
	logger.Infof("desired certificate size is %v.", len(desiredCerts))
	if err != nil {
		return err
	}

	for _, desired := range desiredCerts {
		original, err := c.certificateLister.Certificates(desired.Namespace).Get(desired.Name)
		if apierrs.IsNotFound(err) {
			logger.Infof("There is no certificate for Route %s. Requesting a new certificate.", route.Name)
			if _, err := c.certManagerClientSet.CertmanagerV1alpha1().Certificates(desired.Namespace).Create(desired); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			// TODO(zhiminx): check if the original certificate is ready. If it is ready, we should update the cert configMap
			// to support the TLS with the ready cert.

			if !equality.Semantic.DeepEqual(original.Spec, desired.Spec) {
				existing := original.DeepCopy()
				existing.Spec = desired.Spec
				_, err := c.certManagerClientSet.CertmanagerV1alpha1().Certificates(existing.Namespace).Update(existing)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Reconciler) buildDesiredCerts(ctx context.Context, route *v1alpha1.Route) ([]*certmanagerv1alpha1.Certificate, error) {
	result := []*certmanagerv1alpha1.Certificate{}
	cert, err := c.buildNamespaceLevelCert(ctx, route)
	if err != nil {
		return nil, err
	}
	result = append(result, cert)
	if hasTargetName(route) {
		cert, err := c.buildRouteLevelCert(ctx, route)
		logger := logging.FromContext(ctx)
		logger.Infof("created certificate is %v.", cert)
		if err != nil {
			return nil, err
		}
		result = append(result, cert)
	}
	return result, nil
}

func hasTargetName(route *v1alpha1.Route) bool {
	for i := range route.Spec.Traffic {
		if len(route.Spec.Traffic[i].Name) != 0 {
			return true
		}
	}
	return false
}

func (c *Reconciler) buildNamespaceLevelCert(ctx context.Context, route *v1alpha1.Route) (*certmanagerv1alpha1.Certificate, error) {
	routeHost := routereconciler.RouteDomain(ctx, route)
	wildcardHost := convertToWildcardHost(routeHost)
	if desired, err := getCertificateBuilder(route).Build(ctx, route, []string{wildcardHost}, buildCertName(wildcardHost)); err != nil {
		return nil, err
	} else {
		return desired, nil
	}
}

func (c *Reconciler) buildRouteLevelCert(ctx context.Context, route *v1alpha1.Route) (*certmanagerv1alpha1.Certificate, error) {
	routeHost := routereconciler.RouteDomain(ctx, route)
	wildcardHost := fmt.Sprintf("*.%s", routeHost)
	if desired, err := getCertificateBuilder(route).Build(ctx, route, []string{wildcardHost}, buildCertName(wildcardHost)); err != nil {
		return nil, err
	} else {
		// TODO(zhiminx): add label for the route level cert.
		return desired, nil
	}
}

func buildCertName(host string) string {
	splits := strings.Split(host, ".")
	return fmt.Sprintf("%s", strings.Join(splits[1:], "-"))
}

func convertToWildcardHost(host string) string {
	splits := strings.Split(host, ".")
	return fmt.Sprintf("*.%s", strings.Join(splits[1:], "."))
}

func (c *Reconciler) enqueueWithCertManagerLabel(impl *controller.Impl) func(obj interface{}) {
	return func(obj interface{}) {
		route, ok := obj.(*v1alpha1.Route)
		if !ok {
			c.Logger.Infof("Ignoring non-Route objects %v", obj)
			return
		}
		// TODO(zhiminx): add more logic to filter unrelated Route.
		impl.EnqueueKey(fmt.Sprintf("%s/%s", route.Namespace, route.Name))
	}
}

func (c *Reconciler) enqueueCreatorRoute(impl *controller.Impl) func(obj interface{}) {
	return func(obj interface{}) {
		// TODO(zhiminx): add the Route associated with a certificate into work queue.
		return
	}
}

func getCertificateBuilder(route *v1alpha1.Route) CertificateBuilder {
	// TODO(zhiminx): pick up appropriate builder for the input Route.
	return GcpCertificateBuilder{}
}
