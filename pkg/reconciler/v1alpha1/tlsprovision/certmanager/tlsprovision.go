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

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions/certmanager/v1alpha1"
	certmanagerlisters "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1alpha1"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	servinginformers "github.com/knative/serving/pkg/client/informers/externalversions/serving/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

const controllerAgentName = "tls-certmanager-controller"

type CertificateCreator interface {
	Create(ctx context.Context, route *v1alpha1.Route, hosts []string) (*certmanagerv1alpha1.Certificate, error)
}

// Reconciler implements the logic of requesting certificate for new Route.
type Reconciler struct {
	*reconciler.Base
	routeLister        listers.RouteLister
	certificateLister  certmanagerlisters.CertificateLister
	certificateCreator *CertificateCreator
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	routeInformer servinginformers.RouteInformer,
	certificateInformer certmanagerinformers.CertificateInformer,
	certificateCreator *CertificateCreator,
) *controller.Impl {

	c := &Reconciler{
		Base:               reconciler.NewBase(opt, controllerAgentName),
		routeLister:        routeInformer.Lister(),
		certificateLister:  certificateInformer.Lister(),
		certificateCreator: certificateCreator,
	}

	impl := controller.NewImpl(c, c.Logger, "TLSProvisionCertManager")

	c.Logger.Info("Setting up event handlers")
	routeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueWithCertManagerLabel(impl),
		UpdateFunc: controller.PassNew(c.enqueueWithCertManagerLabel(impl)),
		DeleteFunc: c.enqueueWithCertManagerLabel(impl),
	})

	certificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.enqueueCreatorRoute(impl),
		UpdateFunc: controller.PassNew(c.enqueueCreatorRoute(impl)),
	})

	return impl
}

// Reconcile attemps to converge below actual state and desired state by requesting certificates
// from cert-manager.
// Desired State: There is a certificate provided for a given Route.
// Actual State: No certificate is provided.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)

	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	logger := logging.FromContext(ctx)

	// Get the Route resource with this namespace/name
	route, err := c.routeLister.Routes(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logger.Errorf("Route %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}

	uncertifiedHosts := c.getUncertifiedHosts(route)
	return c.reconcileCertificate(ctx, route)
}

func (c *Reconciler) reconcileCertificate(ctx context.Context, route *v1alpha1.Route, uncertifiedHosts []string) error {

	logger := logging.FromContext(ctx)

	// Certificate of a route is created under the same namespace with the same name of the route
	certificate, err := c.certificateLister.Certificates(route.Namespace).Get(route.Name)
	if apierrs.IsNotFound(err) {
		logger.Infof("There is no certificate for Route %s. Requesting a new certificate.", route.Name)
		newCert, err := c.certificateCreator.Create(ctx, route, uncertifiedHosts)
		if newCert != nil {
			logger.Infof("Created a new certificate %s for Route %s", newCert.Name, route.Name)
		}
		return err
	}
	if err != nil {
		return err
	}

	// If there is an existing certificate requested by the Route, then we don't need to request again.
	if c.isCertificateReady(certificate) {
		// TODO(zhiminx): update the certificate configMap.
		return nil
	}

	// TODO(zhiminx): recover the certificate from error status.
	logger.Infof("The status of the certificate is %v.", certificate.Status.Conditions)
	return nil
}

func (c *Reconciler) getUncertifiedHosts(route *v1alpha1.Route) []string {
	// TODO(zhiminx): implement
	return nil
}

func (c *Reconciler) isCertificateReady(certificate *certmanagerv1alpha1.Certificate) bool {
	// TODO(zhiminx): implement this.
	return true
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
