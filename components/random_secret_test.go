/*
Copyright 2020 Noah Kantrowitz

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

package components

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/tests"
)

type exposeDataComponent struct {
	dest *core.ContextData
}

func (comp *exposeDataComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	*comp.dest = ctx.Data
	return core.Result{}, nil
}

var _ = Describe("RandomSecret component", func() {
	var contextData core.ContextData
	var exposeDataComp core.Component
	var helper *tests.FunctionalHelper
	var obj runtime.Object

	BeforeEach(func() {
		exposeDataComp = &exposeDataComponent{dest: &contextData}
		obj = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
		}
	})

	AfterEach(func() {
		if helper != nil {
			helper.MustStop()
		}
		helper = nil
	})

	It("creates a secret", func() {
		comp := NewRandomSecretComponent("random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret)
		Expect(secret.Data).To(HaveKeyWithValue("key", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key", BeEquivalentTo(secret.Data["key"])))
	})

	It("uses password as the default key", func() {
		comp := NewRandomSecretComponent("random")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret)
		Expect(secret.Data).To(HaveKeyWithValue("password", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("password", BeEquivalentTo(secret.Data["password"])))
	})

	It("creates multiple keys", func() {
		comp := NewRandomSecretComponent("random", "key1", "key2")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret)
		Expect(secret.Data).To(HaveKeyWithValue("key1", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key1", BeEquivalentTo(secret.Data["key1"])))
		Expect(secret.Data).To(HaveKeyWithValue("key2", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key2", BeEquivalentTo(secret.Data["key2"])))
		Expect(secret.Data["key1"]).ToNot(Equal(secret.Data["key2"]))
	})

	It("handles a dynamic name", func() {
		comp := NewRandomSecretComponent("%s-random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("testing-random", secret)
		Expect(secret.Data).To(HaveKeyWithValue("key", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key", BeEquivalentTo(secret.Data["key"])))
	})

	It("updates an existing blank secret", func() {
		comp := NewRandomSecretComponent("random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		preSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "random"},
		}
		c.Create(preSecret)

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret, c.EventuallyValue(Not(BeEmpty()), func(obj runtime.Object) (interface{}, error) {
			return obj.(*corev1.Secret).Data, nil
		}))
		Expect(secret.Data).To(HaveKeyWithValue("key", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key", BeEquivalentTo(secret.Data["key"])))
	})

	It("updates an existing empty key", func() {
		comp := NewRandomSecretComponent("random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		preSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "random"},
			Data: map[string][]byte{
				"key": []byte(""),
			},
		}
		c.Create(preSecret)

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret, c.EventuallyValue(HaveKeyWithValue("key", Not(BeEmpty())), func(obj runtime.Object) (interface{}, error) {
			return obj.(*corev1.Secret).Data, nil
		}))
		Expect(secret.Data).To(HaveKeyWithValue("key", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key", BeEquivalentTo(secret.Data["key"])))
	})

	It("does not update other existing keys", func() {
		comp := NewRandomSecretComponent("random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		preSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "random"},
			Data: map[string][]byte{
				"other": []byte("foo"),
			},
		}
		c.Create(preSecret)

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret, c.EventuallyValue(HaveKeyWithValue("key", Not(BeEmpty())), func(obj runtime.Object) (interface{}, error) {
			return obj.(*corev1.Secret).Data, nil
		}))
		Expect(secret.Data).To(HaveKeyWithValue("key", HaveLen(43)))
		Expect(contextData).To(HaveKeyWithValue("key", BeEquivalentTo(secret.Data["key"])))
		Expect(secret.Data).To(HaveKeyWithValue("other", Equal([]byte("foo"))))
	})

	It("cleans up the secret if the owner is deleted", func() {
		comp := NewRandomSecretComponent("random", "key")
		helper = startTestController(comp, exposeDataComp)
		c := helper.TestClient

		c.Create(obj)

		secret := &corev1.Secret{}
		c.EventuallyGetName("random", secret)

		c.Delete(obj)

		Eventually(func() bool {
			err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "random", Namespace: helper.Namespace}, secret)
			return kerrors.IsNotFound(err)
		}).Should(BeTrue())
	})
})
