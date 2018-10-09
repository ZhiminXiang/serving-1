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

        "github.com/knative/pkg/apis/duck"
        duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
        metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient:nonNamespaced

// DNSRecord represents a DNS record for mapping a source domain to a target IP or domain.
type DNSRecord struct {

        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ObjectMeta `json:"metadata,omitempty"`

        // Spec is the desired state of the DNSRecord.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Spec DNSRecordSpec `json:"spec,omitempty"`

        // Status is the current state of the DNSRecord.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
        // +optional
        Status DNSRecordStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DNSRecordList is a collection of DNSRecord.
type DNSRecordList struct {
        metav1.TypeMeta `json:",inline"`

        // Standard object's metadata.
        // More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
        // +optional
        metav1.ListMeta `json:"metadata,omitempty"`

        // Items is the list of DNSRecord.
        Items []DNSRecord `json:"items"`
}

func (dr *DNSRecord) GetGroupVersionKind() schema.GroupVersionKind {
        return SchemeGroupVersion.WithKind("DNSRecord")
}

// DNSRecordSpec describes the DNSRecord the user wishes to exist.
type DNSRecordSpec struct {

        // TODO: Generation does not work correctly with CRD. They are scrubbed
        // by the APIserver (https://github.com/kubernetes/kubernetes/issues/58778)
        // So, we add Generation here. Once that gets fixed, remove this and use
        // ObjectMeta.Generation instead.
        // +optional
        Generation int64 `json:"generation,omitempty"`

        // A source domain of the DNS record.
        Domain string `json:"domain, omitempty"`

        // Target the source domain will map to. It can be an IP address or another domain.
        Target string `json:"target,omitempty"`
}


// DNSRecordStatus describe the current state of the DNSRecord.
type DNSRecordStatus struct {

        // +optional
        Conditions duckv1alpha1.Conditions `json:"conditions,omitempty"`

        // The observed generation of DNSRecord.
        ObservedGeneration `json:"observedGeneration,omitempty"`

        // The reference to the resources related to the DNS record.
        ResourceReference *core.ObjectReference `json:"resourceReference,omitempty"`
}

// ConditionType represents a DNSRecord condition value
const (
    // DNSRecordReady is set when the source domain was successfully mapped to the target IP or domain.
    DNSRecordReady = duckv1alpha1.ConditionReady
)

var _ apis.Validatable = (*DNSRecord)(nil)
var _ apis.Defaultable = (*DNSRecord)(nil)

// Check that DNSRecord implements the Conditions duck type.
var _ = duck.VerifyType(&DNSRecord{}, &duckv1alpha1.Conditions{})

// Check that DNSRecord implements the Generation duck type.
var emptyGen duckv1alpha1.Generation
var _ = duck.VerifyType(&DNSRecord{}, &emptyGen)

var dnsCondSet = duckv1alpha1.NewLivingConditionSet()

func (drs *DNSRecordStatus) GetConditions() duckv1alpha1.Conditions {
    return drs.Conditions
}

func (drs *DNSRecordStatus) GetCondition(t duckv1alpha1.ConditionType) *duckv1alpha1.Condition {
    return dnsCondSet.Manage(drs).GetCondition(t)
}

func (drs *DNSRecordStatus) InitializeConditions() {
    dnsCondSet.Manage(drs).InitializeConditions()
}

func (drs *DNSRecordStatus) IsReady() bool {
    return dnsCondSet.Manage(drs).IsHappy()
}
