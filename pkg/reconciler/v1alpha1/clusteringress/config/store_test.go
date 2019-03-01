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

package config

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/knative/serving/pkg/network"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

func TestStoreLoadWithContext(t *testing.T) {
	defer ClearAllLoggers()
	store := NewStore(TestLogger(t))

	istioConfig := ConfigMapFromTestFile(t, IstioConfigName)
	networkConfig := ConfigMapFromTestFile(t, network.ConfigName)
	store.OnConfigChanged(istioConfig)
	store.OnConfigChanged(networkConfig)
	config := FromContext(store.ToContext(context.Background()))

	expectedIstio, _ := NewIstioFromConfigMap(istioConfig)
	if diff := cmp.Diff(expectedIstio, config.Istio); diff != "" {
		t.Errorf("Unexpected istio config (-want, +got): %v", diff)
	}

	expectNetworkConfig, _ := network.NewConfigFromConfigMap(networkConfig)
	if diff := cmp.Diff(expectNetworkConfig.TLSMode, config.TLSMode); diff != "" {
		t.Errorf("Unexpected TLS mode (-want, +got): %s", diff)
	}
}

func TestStoreImmutableConfig(t *testing.T) {
	defer ClearAllLoggers()
	store := NewStore(TestLogger(t))

	store.OnConfigChanged(ConfigMapFromTestFile(t, IstioConfigName))
	store.OnConfigChanged(ConfigMapFromTestFile(t, network.ConfigName))

	config := store.Load()

	config.Istio.IngressGateways = []Gateway{{GatewayName: "mutated", ServiceURL: "mutated"}}
	config.TLSMode = network.AUTO

	newConfig := store.Load()

	if newConfig.Istio.IngressGateways[0].GatewayName == "mutated" {
		t.Error("Istio config is not immutable")
	}

	if newConfig.TLSMode == network.AUTO {
		t.Error("TLS Mode is not immutable")
	}
}
