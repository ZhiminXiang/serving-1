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

// AutoDnsRecord represents a DNS record for mapping a domain to an external IP address that wll
// be automatically added into user's DNS server by Knative.
type AutoDnsRecord struct {

        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ObjectMeta `json:"metadata,omitempty"`

        // Spec is the desired state of the AutoDnsRecord.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Spec AutoDnsRecordSpec `json:"spec,omitempty"`

        // Status is the current state of the AutoDnsRecord.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Status AutoDnsRecordStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AutoDnsRecordList is a collection of AutoDnsRecord.
type AutoDnsRecordList struct {
        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ListMeta `json:"metadata,omitempty"`

        // Items is the list of AutoDnsRecord.
        Items []AutoDnsRecord `json:"items"`
}

func (ad *AutoDnsRecord) GetGeneration() int64 {
        return ad.Spec.Generation
}

func (ad *AutoDnsRecord) SetGeneration(generation int64) {
        ad.Spec.Generation = generation
}

func (ad *AutoDnsRecord) GetSpecJSON() ([]byte, error) {
        return json.Marshal(ad.Spec)
}

func (ad *AutoDnsRecord) GetGroupVersionKind() schema.GroupVersionKind {
        return SchemeGroupVersion.WithKind("AutoDnsRecord")
}

// AutoDnsRecordSpec describes the AutoDnsRecord the user wishes to exist.
type AutoDnsRecordSpec struct {

        // TODO: Generation does not work correctly with CRD. They are scrubbed
        // by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
        // So, we add Generation here. Once that gets fixed, remove this and use
        // ObjectMeta.Generation instead.
        // +optional
        Generation int64 `json:"generation,omitempty"`

        // Domain of the DNS record.
        Domain string `json:"domain, omitempty"`

        // IP address the domain will map to.
        IP string `json:"ip,omitempty" protobuf:"bytes,1,opt,name=ip"`
}

var autoDnsCondSet = duckv1alpha1.NewLivingConditionSet()

// AutoDnsRecordStatus describe the current state of the AutoDnsRecord.
type AutoDnsRecordStatus struct {
        // +optional
        Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`
}

// ConditionType represents a AutoDnsRecord condition value
const (
    // AutoDnsRecordReady is set when the DNS record is successfully added into DNS server.
    AutoDnsRecordReady = duckv1alpha1.ConditionReady
)

func (ads *AutoDnsRecordStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
    return autoDnsCondSet.Manage(ads).GetCondition(t)
}

func (ads *AutoDnsRecordStatus) InitializeConditions() {
    autoDnsCondSet.Manage(ads).InitializeConditions()
}

...