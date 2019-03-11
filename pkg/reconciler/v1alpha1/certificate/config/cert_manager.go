/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	yaml "gopkg.in/yaml.v2"

	certmanagerv1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

const (
	acmeKey      = "amce"
	issuerRefKey = "issuerRef"

	// CertManagerConfigName is the name of the configmap containing all
	// configuration related to Cert-Manager.
	CertManagerConfigName = "config-certmanager"
)

// CertManagerConfig contains Cert-Manager related configuration defined in the
// `config-certmanager` config map.
type CertManagerConfig struct {
	ACME      *certmanagerv1alpha1.ACMECertificateConfig
	IssuerRef *certmanagerv1alpha1.ObjectReference
}

// NewCertManagerConfigFromConfigMap creates an CertManagerConfig from the supplied ConfigMap
func NewCertManagerConfigFromConfigMap(configMap *corev1.ConfigMap) (*CertManagerConfig, error) {
	acme := &certmanagerv1alpha1.ACMECertificateConfig{}
	issuerRef := &certmanagerv1alpha1.ObjectReference{}
	for k, v := range configMap.Data {
		if k == acmeKey {
			if err := yaml.Unmarshal([]byte(v), acme); err != nil {
				return nil, err
			}
		} else if k == issuerRefKey {
			if err := yaml.Unmarshal([]byte(v), issuerRef); err != nil {
				return nil, err
			}
		}
	}
	return &CertManagerConfig{
		ACME:      acme,
		IssuerRef: issuerRef,
	}, nil
}
