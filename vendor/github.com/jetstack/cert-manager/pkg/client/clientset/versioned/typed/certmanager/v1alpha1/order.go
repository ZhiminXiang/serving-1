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
	scheme "github.com/jetstack/cert-manager/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// OrdersGetter has a method to return a OrderInterface.
// A group's client should implement this interface.
type OrdersGetter interface {
	Orders(namespace string) OrderInterface
}

// OrderInterface has methods to work with Order resources.
type OrderInterface interface {
	Create(*v1alpha1.Order) (*v1alpha1.Order, error)
	Update(*v1alpha1.Order) (*v1alpha1.Order, error)
	UpdateStatus(*v1alpha1.Order) (*v1alpha1.Order, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.Order, error)
	List(opts v1.ListOptions) (*v1alpha1.OrderList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Order, err error)
	OrderExpansion
}

// orders implements OrderInterface
type orders struct {
	client rest.Interface
	ns     string
}

// newOrders returns a Orders
func newOrders(c *CertmanagerV1alpha1Client, namespace string) *orders {
	return &orders{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the order, and returns the corresponding order object, and an error if there is any.
func (c *orders) Get(name string, options v1.GetOptions) (result *v1alpha1.Order, err error) {
	result = &v1alpha1.Order{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("orders").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of Orders that match those selectors.
func (c *orders) List(opts v1.ListOptions) (result *v1alpha1.OrderList, err error) {
	result = &v1alpha1.OrderList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("orders").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested orders.
func (c *orders) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("orders").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a order and creates it.  Returns the server's representation of the order, and an error, if there is any.
func (c *orders) Create(order *v1alpha1.Order) (result *v1alpha1.Order, err error) {
	result = &v1alpha1.Order{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("orders").
		Body(order).
		Do().
		Into(result)
	return
}

// Update takes the representation of a order and updates it. Returns the server's representation of the order, and an error, if there is any.
func (c *orders) Update(order *v1alpha1.Order) (result *v1alpha1.Order, err error) {
	result = &v1alpha1.Order{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("orders").
		Name(order.Name).
		Body(order).
		Do().
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().

func (c *orders) UpdateStatus(order *v1alpha1.Order) (result *v1alpha1.Order, err error) {
	result = &v1alpha1.Order{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("orders").
		Name(order.Name).
		SubResource("status").
		Body(order).
		Do().
		Into(result)
	return
}

// Delete takes name of the order and deletes it. Returns an error if one occurs.
func (c *orders) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("orders").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *orders) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("orders").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched order.
func (c *orders) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.Order, err error) {
	result = &v1alpha1.Order{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("orders").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
