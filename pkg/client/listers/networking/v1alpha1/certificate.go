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
	v1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// CertificateLister helps list Certificates.
type CertificateLister interface {
	// List lists all Certificates in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.Certificate, err error)
	// Get retrieves the Certificate from the index for a given name.
	Get(name string) (*v1alpha1.Certificate, error)
	CertificateListerExpansion
}

// certificateLister implements the CertificateLister interface.
type certificateLister struct {
	indexer cache.Indexer
}

// NewCertificateLister returns a new CertificateLister.
func NewCertificateLister(indexer cache.Indexer) CertificateLister {
	return &certificateLister{indexer: indexer}
}

// List lists all Certificates in the indexer.
func (s *certificateLister) List(selector labels.Selector) (ret []*v1alpha1.Certificate, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Certificate))
	})
	return ret, err
}

// Get retrieves the Certificate from the index for a given name.
func (s *certificateLister) Get(name string) (*v1alpha1.Certificate, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("certificate"), name)
	}
	return obj.(*v1alpha1.Certificate), nil
}
