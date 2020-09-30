/*
Copyright 2020 Noah Kantrowitz
Copyright 2019 Ridecell, Inc.

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

package tests

import (
	"context"
	"reflect"
	"time"

	"github.com/onsi/gomega"
	gtypes "github.com/onsi/gomega/types"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The default timeout for EventuallyGet().
var DefaultTimeout = 30 * time.Second

// Implementation to match controller-runtime's client.Client interface.
type testClient struct {
	client    client.Client
	namespace string
}

type testStatusClient struct {
	client    client.StatusWriter
	namespace string
}

func defaultNamespace(obj runtime.Object, namespace string) {
	metaobj := obj.(metav1.Object)
	if namespace != "" && metaobj.GetNamespace() == "" {
		metaobj.SetNamespace(namespace)
	}
}

// Implementation used by Get and GetName to keep the stack depth the same.
func (c *testClient) get(key client.ObjectKey, obj runtime.Object) {
	if c.namespace != "" && key.Namespace == "" {
		key.Namespace = c.namespace
	}
	err := c.client.Get(context.Background(), key, obj)
	gomega.ExpectWithOffset(2, err).ToNot(gomega.HaveOccurred())
}

func (c *testClient) Get(key client.ObjectKey, obj runtime.Object) {
	c.get(key, obj)
}

func (c *testClient) GetName(name string, obj runtime.Object) {
	gomega.ExpectWithOffset(1, c.namespace).ToNot(gomega.Equal(""), "Test client namespace not set")
	key := types.NamespacedName{Name: name, Namespace: c.namespace}
	c.get(key, obj)
}

func (c *testClient) List(list runtime.Object, opts ...client.ListOption) {
	defaultNamespace(list, c.namespace)
	err := c.client.List(context.Background(), list, opts...)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

func (c *testClient) Create(obj runtime.Object) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Create(context.Background(), obj)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

func (c *testClient) Delete(obj runtime.Object, opts ...client.DeleteOption) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Delete(context.Background(), obj, opts...)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

func (c *testClient) Update(obj runtime.Object) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Update(context.Background(), obj)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

func (c *testClient) Patch(obj runtime.Object, patch client.Patch, opts ...client.PatchOption) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Patch(context.Background(), obj, patch, opts...)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

// Implementation to match StatusClient.
func (c *testClient) Status() *testStatusClient {
	return &testStatusClient{client: c.client.Status(), namespace: c.namespace}
}

func (c *testStatusClient) Update(obj runtime.Object) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Update(context.Background(), obj)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

func (c *testStatusClient) Patch(obj runtime.Object, patch client.Patch, opts ...client.PatchOption) {
	defaultNamespace(obj, c.namespace)
	err := c.client.Patch(context.Background(), obj, patch, opts...)
	gomega.ExpectWithOffset(1, err).ToNot(gomega.HaveOccurred())
}

// Flexible helper, mostly used for waiting for an object to be available.
type eventuallyGetOptions struct {
	timeout     time.Duration
	valueGetter EventuallyGetValueGetter
	matcher     gtypes.GomegaMatcher
}

type eventuallyGetOptionsSetter func(*eventuallyGetOptions)
type EventuallyGetValueGetter func(runtime.Object) (interface{}, error)

// Set the timeout to a non-default value for EventuallyGet().
func (_ *testClient) EventuallyTimeout(timeout time.Duration) eventuallyGetOptionsSetter {
	return func(o *eventuallyGetOptions) {
		o.timeout = timeout
	}
}

// Set a value getter, to poll until the requested value matches.
func (_ *testClient) EventuallyValue(matcher gtypes.GomegaMatcher, getter EventuallyGetValueGetter) eventuallyGetOptionsSetter {
	return func(o *eventuallyGetOptions) {
		o.matcher = matcher
		o.valueGetter = getter
	}
}

// A common case of a value getter for status conditions.
func (c *testClient) EventuallyCondition(conditionType string, status string) eventuallyGetOptionsSetter {
	return c.EventuallyValue(gomega.Equal(status), func(obj runtime.Object) (interface{}, error) {
		// Yes using reflect is kind of gross but it's test-only code so meh. Some day this can be a bit less reflect-y and use metav1.Condition, when everything is on that.
		conditions := reflect.ValueOf(obj).Elem().FieldByName("Status").FieldByName("Conditions")
		count := conditions.Len()
		for i := 0; i < count; i++ {
			cond := conditions.Index(i)
			if cond.FieldByName("Type").String() == conditionType {
				return cond.FieldByName("Status").String(), nil
			}
		}
		return nil, errors.Errorf("Condition type %s not found", conditionType)
	})
}

// Even more common case of Ready and True.
func (c *testClient) EventuallyReady() eventuallyGetOptionsSetter {
	return c.EventuallyCondition("Ready", "True")
}

// Implementation used by EventuallyGet and EventuallyGetName, to keep the stack depth the same.
func (c *testClient) eventuallyGet(key client.ObjectKey, obj runtime.Object, optSetters ...eventuallyGetOptionsSetter) {
	if c.namespace != "" && key.Namespace == "" {
		key.Namespace = c.namespace
	}
	opts := eventuallyGetOptions{timeout: DefaultTimeout}
	for _, optSetter := range optSetters {
		optSetter(&opts)
	}

	if opts.valueGetter != nil {
		gomega.EventuallyWithOffset(2, func() (interface{}, error) {
			var value interface{}
			err := c.client.Get(context.Background(), key, obj)
			if err == nil {
				value, err = opts.valueGetter(obj)
			}
			return value, err
		}, opts.timeout).Should(opts.matcher)
	} else {
		gomega.EventuallyWithOffset(2, func() error {
			return c.client.Get(context.Background(), key, obj)
		}, opts.timeout).Should(gomega.Succeed())
	}
}

// Like a normal Get but run in a loop. By default it will wait until the call succeeds, but can optionally wait for a value.
func (c *testClient) EventuallyGet(key client.ObjectKey, obj runtime.Object, optSetters ...eventuallyGetOptionsSetter) {
	c.eventuallyGet(key, obj, optSetters...)
}

// EventuallyGet but taking just a name.
func (c *testClient) EventuallyGetName(name string, obj runtime.Object, optSetters ...eventuallyGetOptionsSetter) {
	c.eventuallyGet(types.NamespacedName{Name: name}, obj, optSetters...)
}
