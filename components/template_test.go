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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/coderanger/controller-utils/tests"
)

var _ = Describe("Template component", func() {
	var helper *tests.FunctionalHelper
	var obj runtime.Object

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
		comp := NewTemplateComponent("deployment.yml", nil)
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
		comp := NewTemplateComponent("deployment.yml", nil)
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
		c.EventuallyGetName("testing-webserver", deployment, c.EventuallyValue(PointTo(BeEquivalentTo(0)), func(obj runtime.Object) (interface{}, error) {
			return obj.(*appsv1.Deployment).Spec.Replicas, nil
		}))
		Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(0)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("webserver"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx"))
		Expect(deployment.Spec.MinReadySeconds).To(BeEquivalentTo(42))
	})

	It("sets a status condition", func() {
		comp := NewTemplateComponent("deployment.yml", TemplateConditionGetter("DeploymentAvailable", "Available"))
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)

		c.EventuallyGetName("testing", obj, c.EventuallyCondition("DeploymentAvailable", "Unknown"))

		deployment := &appsv1.Deployment{}
		c.GetName("testing-webserver", deployment)
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
})
