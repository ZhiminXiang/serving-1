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

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GcpCertificateBuilder struct{}

func (gc GcpCertificateBuilder) Build(ctx context.Context, route *v1alpha1.Route, hosts []string, certName string) (*certmanagerv1alpha1.Certificate, error) {
	cert := &certmanagerv1alpha1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: route.Namespace,
			// TODO(zhiminx): add a label here.
		},
		Spec: certmanagerv1alpha1.CertificateSpec{
			SecretName: certName,
			CommonName: hosts[0],
			DNSNames:   hosts,
			IssuerRef: certmanagerv1alpha1.ObjectReference{
				Kind: "ClusterIssuer",
				Name: "letsencrypt-issuer",
			},
			ACME: &certmanagerv1alpha1.ACMECertificateConfig{
				Config: []certmanagerv1alpha1.DomainSolverConfig{
					{
						Domains: hosts,
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

	return cert, nil
}
