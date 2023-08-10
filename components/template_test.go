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
	. "github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/tests"
)

type injectDataComponent struct {
	key   string
	value string
}

func (comp *injectDataComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	ctx.Data[comp.key] = comp.value
	return core.Result{}, nil
}

var _ core.Component = &injectDataComponent{}

var _ = Describe("Template component", func() {
	var helper *tests.FunctionalHelper
	var obj *TestObject

	BeforeEach(func() {
		obj = &TestObject{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
		}
	})

	AfterEach(func() {
		if helper != nil {
			helper.MustStop()
		}
		helper = nil
	})

	It("creates a deployment", func() {
		comp := NewTemplateComponent("deployment.yml", "")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)

		deployment := &appsv1.Deployment{}
		c.EventuallyGetName("testing-webserver", deployment)
		Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(0)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("webserver"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx"))
	})

	It("overwrites fields controlled by the template but not others", func() {
		comp := NewTemplateComponent("deployment.yml", "")
		helper = startTestController(comp)
		c := helper.TestClient

		replicas := int32(1)
		preDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "testing-webserver"},
			Spec: appsv1.DeploymentSpec{
				Replicas:        &replicas,
				MinReadySeconds: 42,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "webserver",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "webserver",
								Image: "other",
							},
						},
					},
				},
			},
		}
		c.Create(preDeployment)

		c.Create(obj)

		deployment := &appsv1.Deployment{}
		c.EventuallyGetName("testing-webserver", deployment, c.EventuallyValue(PointTo(BeEquivalentTo(0)), func(obj client.Object) (interface{}, error) {
			return obj.(*appsv1.Deployment).Spec.Replicas, nil
		}))
		Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(0)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("webserver"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx"))
		Expect(deployment.Spec.MinReadySeconds).To(BeEquivalentTo(42))
	})

	It("sets a status condition", func() {
		comp := NewTemplateComponent("deployment.yml", "DeploymentAvailable")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "Unknown"))

		deployment := &appsv1.Deployment{}
		c.GetName("testing-webserver", deployment)
		Expect(deployment.GetAnnotations()).ToNot(HaveKey(DELETE_ANNOTATION))
		deploymentClean := deployment.DeepCopy()
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   "Available",
				Status: corev1.ConditionFalse,
				Reason: "Fake",
			},
		}
		c.Status().Patch(deployment, client.MergeFrom(deploymentClean))

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "False"))

		c.GetName("testing-webserver", deployment)
		deploymentClean = deployment.DeepCopy()
		deployment.Status.Conditions[0].Status = corev1.ConditionTrue
		c.Status().Patch(deployment, client.MergeFrom(deploymentClean))

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "True"))
	})

	It("deletes an object", func() {
		comp := NewTemplateComponent("deployment.yml", "DeploymentAvailable")
		helper = startTestController(comp)
		c := helper.TestClient

		controller := true
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-webserver",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "test.coderanger.net/v1",
						Kind:       "TestObject",
						Name:       "testing",
						Controller: &controller,
						UID:        "fake", // Not used but must be filled in.
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "webserver",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "default",
								Image: "something",
							},
						},
					},
				},
			},
		}
		c.Create(deployment)

		obj.Spec.Field = "true"
		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "True"))

		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing-webserver", Namespace: helper.Namespace}, deployment)
		Expect(err).To(HaveOccurred())
		Expect(kerrors.IsNotFound(err)).To(BeTrue())
	})

	It("does not delete an unowned object", func() {
		comp := NewTemplateComponent("deployment.yml", "DeploymentAvailable")
		helper = startTestController(comp)
		c := helper.TestClient

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-webserver",
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "webserver",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "default",
								Image: "something",
							},
						},
					},
				},
			},
		}
		c.Create(deployment)

		obj.Spec.Field = "true"
		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "True"))

		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing-webserver", Namespace: helper.Namespace}, deployment)
		Expect(err).ToNot(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("something"))
	})

	It("does not delete an object owned by someone else", func() {
		comp := NewTemplateComponent("deployment.yml", "DeploymentAvailable")
		helper = startTestController(comp)
		c := helper.TestClient

		controller := true
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testing-webserver",
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "test.coderanger.net/v1",
						Kind:       "TestObject",
						Name:       "testing-other",
						Controller: &controller,
						UID:        "fake", // Not used but must be filled in.
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "webserver",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "webserver",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "default",
								Image: "something",
							},
						},
					},
				},
			},
		}
		c.Create(deployment)

		obj.Spec.Field = "true"
		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "True"))

		err := helper.Client.Get(context.Background(), types.NamespacedName{Name: "testing-webserver", Namespace: helper.Namespace}, deployment)
		Expect(err).ToNot(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("something"))
	})

	It("does not delete an already deleted object", func() {
		comp := NewTemplateComponent("deployment.yml", "DeploymentAvailable")
		helper = startTestController(comp)
		c := helper.TestClient

		obj.Spec.Field = "true"
		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "True"))
	})

	It("handles template data", func() {
		dataComp := &injectDataComponent{key: "FOO", value: "bar"}
		comp := NewTemplateComponent("configmap.yml", "")
		helper = startTestController(dataComp, comp)
		c := helper.TestClient

		c.Create(obj)

		cmap := &corev1.ConfigMap{}
		c.EventuallyGetName("testing", cmap)
		Expect(cmap.Data).To(HaveKeyWithValue("FOO", Equal("bar")))
	})
})
