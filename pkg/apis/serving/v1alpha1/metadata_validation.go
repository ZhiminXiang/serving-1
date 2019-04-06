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

package v1alpha1

import (
	"fmt"

	"github.com/knative/pkg/apis"
	"github.com/knative/serving/pkg/apis/autoscaling"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ValidateObjectMetadata validates that `metadata` stanza of the
// resources is correct.
func ValidateObjectMetadata(meta metav1.Object) *apis.FieldError {
	name := meta.GetName()
	generateName := meta.GetGenerateName()

	if generateName != "" {
		msgs := validation.NameIsDNS1035Label(generateName, true)

		if len(msgs) > 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("not a DNS 1035 label prefix: %v", msgs),
				Paths:   []string{"generateName"},
			}
		}
	}

	if name != "" {
		msgs := validation.NameIsDNS1035Label(name, false)

		if len(msgs) > 0 {
			return &apis.FieldError{
				Message: fmt.Sprintf("not a DNS 1035 label: %v", msgs),
				Paths:   []string{"name"},
			}
		}
	}

	if generateName == "" && name == "" {
		return &apis.FieldError{
			Message: "name or generateName is required",
			Paths:   []string{"name"},
		}
	}

	if err := autoscaling.ValidateAnnotations(meta.GetAnnotations()); err != nil {
		return err.ViaField("annotations")
	}

	return nil
}
