/*
Copyright 2019 The Knative Authors.

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

package tls

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/knative/pkg/system"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	_ "github.com/knative/pkg/system/testing"
)

func TestConfig(t *testing.T) {
	cases := []struct {
		name       string
		config     *corev1.ConfigMap
		wantErr    bool
		wantConfig *Config
	}{{
		name: "enable auto tls",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				"enable-auto-tls": "true",
			},
		},
		wantErr: false,
		wantConfig: &Config{
			EnableAutoTLS: true,
		},
	}, {
		name: "disable auto tls",
		config: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: system.Namespace(),
				Name:      ConfigName,
			},
			Data: map[string]string{
				"enable-auto-tls": "false",
			},
		},
		wantErr: false,
		wantConfig: &Config{
			EnableAutoTLS: false,
		},
	}}

	for _, tt := range cases {
		actualConfig, err := NewConfigFromConfigMap(tt.config)

		if (err != nil) != tt.wantErr {
			t.Fatalf("Test: %q; NewConfigFromConfigMap() error = %v, WantErr %v", tt.name, err, tt.wantErr)
		}

		if diff := cmp.Diff(actualConfig, tt.wantConfig); diff != "" {
			t.Fatalf("Test: %q; want %v, but got %v", tt.name, tt.wantConfig, actualConfig)
		}
	}
}
