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

package certificate

import (
	"testing"

	fakecmclient "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/fake"
	"github.com/knative/pkg/controller"
	"github.com/knative/serving/pkg/reconciler"
	. "github.com/knative/serving/pkg/reconciler/v1alpha1/testing"
)

// This is heavily based on the way the OpenShift Ingress controller tests its reconciliation method.
func TestReconcile(t *testing.T) {
	table := TableTest{{
		Name:                    "bad workqueue key",
		Key:                     "too/many/parts",
		SkipNamespaceValidation: true,
	}}

	table.Test(t, MakeFactory(func(listers *Listers, opt reconciler.Options) controller.Reconciler {
		return &Reconciler{
			Base:                reconciler.NewBase(opt, controllerAgentName),
			knCertificateLister: listers.GetKnCertificateLister(),
			cmCertificateLister: listers.GetCMCertificateLister(),
			certManagerClient:   fakecmclient.NewSimpleClientset(listers.GetCMCertificateObjects()...),
		}
	}))
}
