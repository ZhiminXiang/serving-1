/*
Copyright 2019 The Jetstack cert-manager contributors.

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
	v1alpha1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// OrderLister helps list Orders.
type OrderLister interface {
	// List lists all Orders in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.Order, err error)
	// Orders returns an object that can list and get Orders.
	Orders(namespace string) OrderNamespaceLister
	OrderListerExpansion
}

// orderLister implements the OrderLister interface.
type orderLister struct {
	indexer cache.Indexer
}

// NewOrderLister returns a new OrderLister.
func NewOrderLister(indexer cache.Indexer) OrderLister {
	return &orderLister{indexer: indexer}
}

// List lists all Orders in the indexer.
func (s *orderLister) List(selector labels.Selector) (ret []*v1alpha1.Order, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Order))
	})
	return ret, err
}

// Orders returns an object that can list and get Orders.
func (s *orderLister) Orders(namespace string) OrderNamespaceLister {
	return orderNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// OrderNamespaceLister helps list and get Orders.
type OrderNamespaceLister interface {
	// List lists all Orders in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1alpha1.Order, err error)
	// Get retrieves the Order from the indexer for a given namespace and name.
	Get(name string) (*v1alpha1.Order, error)
	OrderNamespaceListerExpansion
}

// orderNamespaceLister implements the OrderNamespaceLister
// interface.
type orderNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all Orders in the indexer for a given namespace.
func (s orderNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.Order, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.Order))
	})
	return ret, err
}

// Get retrieves the Order from the indexer for a given namespace and name.
func (s orderNamespaceLister) Get(name string) (*v1alpha1.Order, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("order"), name)
	}
	return obj.(*v1alpha1.Order), nil
}
