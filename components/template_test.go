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

	"github.com/coderanger/controller-utils/tests"
)

var _ = Describe("Template component", func() {
	var helper *tests.FunctionalHelper
	var obj runtime.Object

	BeforeEach(func() {
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

	It("creates a deployment", func() {
		comp := NewTemplateComponent("deployment.yml")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)

		deployment := &appsv1.Deployment{}
		c.EventuallyGetName("testing-webserver", deployment)
		Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(0)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Name).To(Equal("webserver"))
	})
})
