/*
Copyright 2018 The Knative Authors.

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

package v1alpha1

import (
        "encoding/json"

        duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced
// 
type TLS struct {

        metav1.TypeMeta `json:",inline"`
        
        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ObjectMeta `json:"metadata,omitempty"`

        // Spec is the desired state of the TLS.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Spec TLSSpec `json:"spec,omitempty"`

        // Status is the current state of the TLS.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Status TLSStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TLSList is a collection of TLS.
type TLSList struct {
        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ListMeta `json:"metadata,omitempty"`

        // Items is the list of ManagedTLSProvision.
        Items []TLS `json:"items"`
}

// TLSSpec describes the state of TLS the user wishes to exist.
type TLSSpec struct {

        // TODO: Generation does not work correctly with CRD. They are scrubbed
        // by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
        // So, we add Generation here. Once that gets fixed, remove this and use
        // ObjectMeta.Generation instead.
        // +optional
        Generation int64 `json:"generation,omitempty"`

        // The domain we want to provide TLS for.
        Domain string `json:"domain,omitempty"`
}

// TLSStatus describe the current state of the TLS.
type TLSStatus struct {

        // +optional condition of the 
        Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`

        // The created TLS resource that is associated with the TLS object.
        ResourceReference CertificateResource `json:"resourceReference,omitempty"`
}

// The certificate resource related to the TLS.
type CertificateResource struct {

        // The name of the certificate resource.
        Name `json:"name,omitempty"`

        // The namespace of the certificate resource.
        Namespace `json:"namespace,omitempty"`

        // The corresponding secret of the certificate. This is optional since
        // certificates may not be stored in the cluster as secrets.
        Secret Secret `json:"secret,omitempty"`

        // Expiration time of the certificate. In the RFC3339 format.
        ExpirationTime metav1.Time `json:"expirationTime,omitempty"`

        // A list of domain names that can be authenticated by the certificate.
        domainNames []string `json:"dnsNames,omitempty"`

        // Description of the certificate.
        Description string `json:"description,omitempty"`
}

type Secret struct {

        // The name of the secret.
        Name string `json:"name,omitempty"`

        // The namespace of the secret.
        Namespace string `json:"namespace,omitempty"`
}

const (
    TLSProvisioning duckv1alpha1.ConditionType = "Provisioning"

    TLSDeleting duckv1alpha1.ConditionType = "Deleting"

    TLSReady duckv1alpha1.ConditionType duckv1alpha1.ConditionReady

    TLSProvisionFailed duckv1alpha1.ConditionType = "Failed"
)
