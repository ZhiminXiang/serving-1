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

// ManagedSslProvision aims to automatically provide TLS certificate for the requested domain in the give ingress. 
type ManagedTLSProvision struct {

        metav1.TypeMeta `json:",inline"`
        
        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ObjectMeta `json:"metadata,omitempty"`

        // Spec is the desired state of the ManagedTLSProvision.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Spec ManagedTLSProvisionSpec `json:"spec,omitempty"`

        // Status is the current state of the ManagedTLSProvisionStatus.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Status ManagedTLSProvisionStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ManagedTLSProvisionList is a collection of ManagedTLSProvision.
type ManagedTLSProvisionList struct {
        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ListMeta `json:"metadata,omitempty"`

        // Items is the list of ManagedTLSProvision.
        Items []ManagedTLSProvision `json:"items"`
}

// ManagedTLSProvisionSpec describes the state of ManagedTLSProvision the user wishes to exist.
type ManagedTLSProvisionSpec struct {

        // TODO: Generation does not work correctly with CRD. They are scrubbed
        // by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
        // So, we add Generation here. Once that gets fixed, remove this and use
        // ObjectMeta.Generation instead.
        // +optional
        Generation int64 `json:"generation,omitempty"`

        // The domain we want to provide TLS for.
        Domain string `json:"domain,omitempty"`

        // The name of the knative ingress that should terminate the SSL connection for the given domain.
        // Currently it should be the ClusterIngress of Knative since we only have one ingress per cluster. 
        TargetIngress Ingress `json:"ingress,omitempty"`
}

type Ingress struct {
        // The name of Ingress.
        Name string `json:"name,omitempty"`

        // The namespace of Ingress.
        Namespace string `json:"name,omitempty"`
}

// ManagedSslProvisionStatus describe the current state of the ManagedTLSProvision.
type ManagedTLSProvisionStatus struct {

        // +optional condition of the 
        Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`

        // The name of the certificate associated with the domain.
        CertificateName string `json:"name,omitempty"`
}

const (
    ManagedTLSProvisionInProgress duckv1alpha1.ConditionType = "Provisioning"

    ManagedTLSProvisionReady duckv1alpha1.ConditionType duckv1alpha1.ConditionReady

    ManagedTLSProvisionFailed duckv1alpha1.ConditionType = "Failed"
)