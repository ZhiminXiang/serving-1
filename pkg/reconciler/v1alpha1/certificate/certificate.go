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

package certificate

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	certmanagerclientset "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions/certmanager/v1alpha1"
	certmanagerlisters "github.com/jetstack/cert-manager/pkg/client/listers/certmanager/v1alpha1"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/kmeta"
	"github.com/knative/pkg/logging"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	informers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	listers "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	controllerAgentName = "certificate-controller"
)

// Reconciler implements controller.Reconciler for Certificate resources.
type Reconciler struct {
	*reconciler.Base

	// listers index properties about resources
	knCertificateLister  listers.CertificateLister
	cmCertificateLister  certmanagerlisters.CertificateLister
	certManagerClientSet certmanagerclientset.Interface
}

// NewController initializes the controller and is called by the generated code
// Registers eventhandlers to enqueue events.
func NewController(
	opt reconciler.Options,
	knCertificateInformer informers.CertificateInformer,
	cmCertificateInformer certmanagerinformers.CertificateInformer,
	certManagerClientSet certmanagerclientset.Interface,
) *controller.Impl {

	c := &Reconciler{
		Base:                 reconciler.NewBase(opt, controllerAgentName),
		knCertificateLister:  knCertificateInformer.Lister(),
		cmCertificateLister:  cmCertificateInformer.Lister(),
		certManagerClientSet: certManagerClientSet,
	}

	impl := controller.NewImpl(c, c.Logger, "Certificate", reconciler.MustNewStatsReporter("Certificate", c.Logger))

	c.Logger.Info("Setting up event handlers")
	knCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.Enqueue,
		UpdateFunc: controller.PassNew(impl.Enqueue),
		DeleteFunc: impl.Enqueue,
	})

	cmCertificateInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    impl.EnqueueControllerOf,
		UpdateFunc: controller.PassNew(impl.EnqueueControllerOf),
		DeleteFunc: impl.EnqueueControllerOf,
	})
	return impl
}

// Check that our Reconciler implements controller.Reconciler
var _ controller.Reconciler = (*Reconciler)(nil)

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Certificate resource
// with the current status of the resource.
func (c *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		c.Logger.Errorf("invalid resource key: %s", key)
		return nil
	}
	logger := logging.FromContext(ctx)

	original, err := c.knCertificateLister.Certificates(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		logger.Errorf("Knative Certificate %q in work queue no longer exists", key)
		return nil
	} else if err != nil {
		return err
	}

	// Don't modify the informers copy
	knCert := original.DeepCopy()

	// Reconcile this copy of the Certificate and then write back any status
	// updates regardless of whether the reconciliation errored out.
	err = c.reconcile(ctx, knCert)
	if equality.Semantic.DeepEqual(original.Status, knCert.Status) {
		// If we didn't change anything then don't call updateStatus.
		// This is important because the copy we loaded from the informer's
		// cache may be stale and we don't want to overwrite a prior update
		// to status with this stale state.
	} else if _, err := c.updateStatus(knCert); err != nil {
		logger.Warn("Failed to update certificate status", zap.Error(err))
		c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "UpdateFailed",
			"Failed to update status for Certificate %q: %v", key, err)
		return err
	}
	return err
}

func (c *Reconciler) reconcile(ctx context.Context, knCert *v1alpha1.Certificate) error {
	logger := logging.FromContext(ctx)

	// TODO(zhiminx): set defaults for knCert
	// TODO(zhiminx): initialize conditions of the status of knCert.

	logger.Info("Reconciling Cert-Manager certificate.")
	cmCert := c.makeCertManagerCertificate(knCert)
	cmCert, err := c.reconcileCMCertificate(ctx, knCert, cmCert)
	if err != nil {
		return err
	}
	setCertStatus(knCert, cmCert)
	return nil
}

func (c *Reconciler) makeCertManagerCertificate(knCert *v1alpha1.Certificate) *certmanagerv1alpha1.Certificate {
	dnsNames := makeWildcardHosts(knCert.Spec.DNSNames)
	sort.Strings(dnsNames)
	// For the DNS Names in knCert, we actually request a certificate with the wildcard format
	// of these DNS Names so that this certificate could be reused.
	return &certmanagerv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      knCert.Name,
			Namespace: knCert.Namespace,
			// TODO(zhiminx): we may not want to add ownerreference because we probably need to cache CM certificates for a while.
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(knCert)},
		},
		Spec: certmanagerv1alpha1.CertificateSpec{
			SecretName: knCert.Spec.SecretName,
			DNSNames:   dnsNames,
			// TODO(zhiminx): put the issuer into ConfigMap.
			IssuerRef: certmanagerv1alpha1.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "letsencrypt-issuer",
			},
			// TODO(zhiminx): put the provider into ConfigMap
			ACME: &certmanagerv1alpha1.ACMECertificateConfig{
				Config: []certmanagerv1alpha1.DomainSolverConfig{
					{
						Domains: dnsNames,
						SolverConfig: certmanagerv1alpha1.SolverConfig{
							DNS01: &certmanagerv1alpha1.DNS01SolverConfig{
								Provider: "cloud-dns-provider",
							},
						},
					},
				},
			},
		},
	}
}

func makeWildcardHosts(dnsNames []string) []string {
	var res []string
	for _, dnsName := range dnsNames {
		splits := strings.Split(dnsName, ".")
		res = append(res, fmt.Sprintf("*.%s", strings.Join(splits[1:], ".")))
	}
	return dedup(res)
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

func (c *Reconciler) reconcileCMCertificate(ctx context.Context, knCert *v1alpha1.Certificate, desired *certmanagerv1alpha1.Certificate) (*certmanagerv1alpha1.Certificate, error) {
	logger := logging.FromContext(ctx)
	cmCert, err := c.cmCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if apierrs.IsNotFound(err) {
		cmCert, err = c.certManagerClientSet.CertmanagerV1alpha1().Certificates(desired.Namespace).Create(desired)
		if err != nil {
			logger.Error("Failed to create Cert-Manager certificate", zap.Error(err))
			c.Recorder.Eventf(knCert, corev1.EventTypeWarning, "CreationFailed",
				"Failed to create Cert-Manager Certificate %q/%q: %v", desired.Name, desired.Namespace, err)
			return nil, err
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Created",
			"Created Cert-Manager Certificate %q/%q", desired.Name, desired.Namespace)
	} else if err != nil {
		return nil, err
	} else if !equality.Semantic.DeepEqual(cmCert.Spec, desired.Spec) {
		origin := cmCert.DeepCopy()
		origin.Spec = desired.Spec
		updated, err := c.certManagerClientSet.CertmanagerV1alpha1().Certificates(origin.Namespace).Update(origin)
		if err != nil {
			logger.Error("Failed to update Cert-Manager Certificate", zap.Error(err))
			return nil, err
		}
		c.Recorder.Eventf(knCert, corev1.EventTypeNormal, "Updated",
			"Updated Spec for Cert-Manager Certificate %q/%q", desired.Name, desired.Namespace)
		return updated, nil
	}
	return cmCert, nil
}

func setCertStatus(knCert *v1alpha1.Certificate, cmCert *certmanagerv1alpha1.Certificate) {
	knCert.Status.CertificateInfo = v1alpha1.CertificateInfo{
		SupportedDNSNames: cmCert.Spec.DNSNames,
		NotAfter:          cmCert.Status.NotAfter,
	}

	if isCmCertificateReady(cmCert) {
		knCert.Status.MarkReady()
	}
}

func isCmCertificateReady(cmCert *certmanagerv1alpha1.Certificate) bool {
	for _, condition := range cmCert.Status.Conditions {
		if condition.Type == certmanagerv1alpha1.CertificateConditionReady {
			return condition.Status == certmanagerv1alpha1.ConditionTrue
		}
	}
	return false
}

func (c *Reconciler) updateStatus(desired *v1alpha1.Certificate) (*v1alpha1.Certificate, error) {
	cert, err := c.knCertificateLister.Certificates(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}
	// If there's nothing to update, just return.
	if reflect.DeepEqual(cert.Status, desired.Status) {
		return cert, nil
	}
	// Don't modify the informers copy
	existing := cert.DeepCopy()
	existing.Status = desired.Status

	return c.ServingClientSet.NetworkingV1alpha1().Certificates(existing.Namespace).UpdateStatus(existing)
}
