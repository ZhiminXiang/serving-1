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

package tls

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// ConfigName is the name of the config map of the TLS.
	ConfigName = "config-tls"
)

// Config contains the TLS configuration defined in the config-tls config map.
// +k8s:deepcopy-gen=true
type Config struct {
	// Feature flags to enable automatic TLS certificate provision and configuration.
	EnableAutoTLS bool
}

// NewConfigFromConfigMap creates a Config from the supplied ConfigMap.
func NewConfigFromConfigMap(configMap *corev1.ConfigMap) (*Config, error) {
	cfg := &Config{}
	// Process bool fields
	for _, b := range []struct {
		key          string
		field        *bool
		defaultValue bool
	}{{
		key:          "enable-auto-tls",
		field:        &cfg.EnableAutoTLS,
		defaultValue: false,
	}} {
		if raw, ok := configMap.Data[b.key]; !ok {
			*b.field = b.defaultValue
		} else {
			*b.field = strings.ToLower(raw) == "true"
		}
	}
	return cfg, nil
}
