/*
Copyright 2019 The Knative Authors

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

package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/knative/pkg/logging"
	"github.com/knative/pkg/system"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/resources/names"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/route/traffic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

// MakeCertificates creates Certificates for the Route to request TLS certificates.
func MakeCertificates(ctx context.Context, route *v1alpha1.Route, dnsNames []string, enableWildcardCert bool) ([]*networkingv1alpha1.Certificate, error) {
	if len(route.Status.Domain) == 0 {
		return nil, fmt.Errorf("the Domain of the Status of Route %s/%s must not be empty", route.Namespace, route.Name)
	}

	logger := logging.FromContext(ctx)
	logger.Infof("DNS names are %q", dnsNames)

	dnsNames = dedup(dnsNames)
	sort.Strings(dnsNames)
	certs := []*networkingv1alpha1.Certificate{}
	if enableWildcardCert {
		existingWildcardNames := sets.String{}
		for _, dnsName := range dnsNames {
			wildcardDNSName := wildcard(dnsName)
			if existingWildcardNames.Has(wildcardDNSName) {
				continue
			}
			existingWildcardNames.Insert(wildcardDNSName)
			certName := wildcardCertName(wildcardDNSName)
			cert, err := makeCert(route, []string{wildcardDNSName}, certName)
			if err != nil {
				return nil, err
			}
			certs = append(certs, cert)
		}
	} else {
		cert, err := makeCert(route, dnsNames, names.RouteCertificate(route))
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, nil
}

func makeCert(route *v1alpha1.Route, dnsNames []string, certName string) (*networkingv1alpha1.Certificate, error) {
	routeNamespaceName, err := json.Marshal([]string{fmt.Sprintf("%s/%s", route.Namespace, route.Name)})
	if err != nil {
		return nil, err
	}
	return &networkingv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name: certName,
			// TODO(zhiminx): make certificate namespace configurable
			Namespace: system.Namespace(),
			Annotations: map[string]string{
				serving.RouteNamespaceNameAnnotationKey: string(routeNamespaceName),
			},
		},
		Spec: networkingv1alpha1.CertificateSpec{
			DNSNames:   dnsNames,
			SecretName: certName,
		},
	}, nil
}

func wildcard(dnsName string) string {
	splits := strings.Split(dnsName, ".")
	return fmt.Sprintf("*.%s", strings.Join(splits[1:], "."))
}

func wildcardCertName(wildcardDNSName string) string {
	splits := strings.Split(wildcardDNSName, ".")
	return strings.Join(splits[1:], ".")
}

func GetDNSNames(route *v1alpha1.Route, tc *traffic.Config) []string {
	dnsNames := []string{route.Status.Domain}
	for name := range tc.Targets {
		if name != traffic.DefaultTarget {
			dnsNames = append(dnsNames, fmt.Sprintf("%s.%s", name, route.Status.Domain))
		}
	}
	return dnsNames
}
func IsCertOwner(cert *networkingv1alpha1.Certificate, route *v1alpha1.Route) (bool, error) {
	var routeKeys []string
	if err := json.Unmarshal([]byte(cert.Annotations[serving.RouteNamespaceNameAnnotationKey]), &routeKeys); err != nil {
		return false, err
	}
	routeKey := fmt.Sprintf("%s/%s", route.Namespace, route.Name)
	for _, key := range routeKeys {
		if key == routeKey {
			return true, nil
		}
	}
	return false, nil
}

func AddToCertOwner(cert *networkingv1alpha1.Certificate, route *v1alpha1.Route) error {
	var routeKeys []string
	if err := json.Unmarshal([]byte(cert.Annotations[serving.RouteNamespaceNameAnnotationKey]), &routeKeys); err != nil {
		return err
	}
	routeKeys = append(routeKeys, fmt.Sprintf("%s/%s", route.Namespace, route.Name))
	sort.Strings(routeKeys)
	annotation, err := json.Marshal(routeKeys)
	if err != nil {
		return err
	}
	cert.Annotations[serving.RouteNamespaceNameAnnotationKey] = string(annotation)
	return nil
}

func GetUnusedCerts(desiredCerts []*networkingv1alpha1.Certificate, route *v1alpha1.Route) []v1alpha1.Certificate {
	desiredKeys := sets.String{}
	for _, desired := range desiredCerts {
		desiredKeys.Insert(fmt.Sprintf("%s/%s", desired.Namespace, desired.Name))
	}
	unusedCerts := []v1alpha1.Certificate{}
	for _, currCert := range route.Status.Certificates {
		if !desiredKeys.Has(fmt.Sprintf("%s/%s", currCert.Namespace, currCert.Name)) {
			unusedCerts = append(unusedCerts, currCert)
		}
	}
	return unusedCerts
}

func RemoveCertOwner(cert *networkingv1alpha1.Certificate, route *v1alpha1.Route) error {
	var routeKeys []string
	if err := json.Unmarshal([]byte(cert.Annotations[serving.RouteNamespaceNameAnnotationKey]), &routeKeys); err != nil {
		return err
	}
	routeKeyset := sets.NewString(routeKeys...)
	routeKeyset.Delete(fmt.Sprintf("%s/%s", route.Namespace, route.Name))
	annotation, err := json.Marshal(routeKeyset.List())
	if err != nil {
		return err
	}
	cert.Annotations[serving.RouteNamespaceNameAnnotationKey] = string(annotation)
	return nil
}
