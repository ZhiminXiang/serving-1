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
	"fmt"
	"strings"

	"github.com/knative/pkg/kmeta"
	"github.com/knative/serving/pkg/apis/networking"
	"github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/config"
	"github.com/knative/serving/pkg/reconciler/v1alpha1/clusteringress/resources/names"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	sets "k8s.io/apimachinery/pkg/util/sets"
	corev1listers "k8s.io/client-go/listers/core/v1"
)

func MakeDesiredSecrets(ctx context.Context, ci *v1alpha1.ClusterIngress, secretLister corev1listers.SecretLister) ([]*corev1.Secret, error) {
	gatewaySvcNamespaces := GetGatewaySvcNamespaces(ctx)
	secrets := []*corev1.Secret{}
	for _, tls := range ci.Spec.TLS {
		originSecret, err := secretLister.Secrets(tls.SecretNamespace).Get(tls.SecretName)
		if err != nil {
			return nil, err
		}
		for _, ns := range gatewaySvcNamespaces {
			secrets = append(secrets, makeTargetSecret(originSecret, ci, ns))
		}
	}
	return secrets, nil
}

func GetGatewaySvcNamespaces(ctx context.Context) []string {
	cfg := config.FromContext(ctx).Istio
	namespaces := []string{}
	for _, ingressgateway := range cfg.IngressGateways {
		ns := strings.Split(ingressgateway.ServiceURL, ".")[1]
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// makeTargetSecret creates a copy of originSecret with the given namespace.
func makeTargetSecret(originSecret *corev1.Secret, ci *v1alpha1.ClusterIngress, gatewayServiceNamespace string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.TargetSecret(originSecret.Namespace, originSecret.Name, ci),
			Namespace:       gatewayServiceNamespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(ci)},
			Labels:          makeLabels(originSecret, ci),
		},
		Data: originSecret.Data,
		Type: originSecret.Type,
	}
}

func makeLabels(originSecret *corev1.Secret, ci *v1alpha1.ClusterIngress) map[string]string {
	return map[string]string{
		networking.IngressLabelKey:               ci.Name,
		networking.OriginSecretNameLabelKey:      originSecret.Name,
		networking.OriginSecretNamespaceLabelKey: originSecret.Namespace,
	}
}

func MakeSecretSelector(ci *v1alpha1.ClusterIngress) labels.Selector {
	return labels.Set(map[string]string{
		networking.IngressLabelKey: ci.Name,
	}).AsSelector()
}

func GetOriginSecrets(ci *v1alpha1.ClusterIngress) sets.String {
	secretKeys := sets.String{}
	for _, tls := range ci.Spec.TLS {
		secretKeys.Insert(fmt.Sprintf("%s/%s", tls.SecretNamespace, tls.SecretName))
	}
	return secretKeys
}

func CopySecrets(secrets []*corev1.Secret) []*corev1.Secret {
	copy := []*corev1.Secret{}
	for _, secret := range secrets {
		copy = append(copy, secret.DeepCopy())
	}
	return copy
}
