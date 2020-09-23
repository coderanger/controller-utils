/*
Copyright 2020 Noah Kantrowitz
Copyright 2018-2019 Ridecell, Inc.

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

package templates_test

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/coderanger/controller-utils/templates"
)

var testTemplates http.FileSystem = http.Dir("test_templates")

var _ = Describe("Templates", func() {
	Context("a simple template", func() {
		It("should render the Deployment", func() {
			rawObject, err := templates.Get(testTemplates, "test1.yml.tpl", false, struct{}{})
			Expect(err).ToNot(HaveOccurred())
			deployment, ok := rawObject.(*appsv1.Deployment)
			Expect(ok).To(BeTrue())
			Expect(deployment.Name).To(Equal("test"))
			Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(1)))
		})
	})

	Context("a helper template", func() {
		It("should render the Deployment", func() {
			rawObject, err := templates.Get(testTemplates, "test2.yml.tpl", false, struct{}{})
			Expect(err).ToNot(HaveOccurred())
			deployment, ok := rawObject.(*appsv1.Deployment)
			Expect(ok).To(BeTrue())
			Expect(deployment.Name).To(Equal("test-two"))
			Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(1)))
		})
	})

	Context("a template variable", func() {
		It("should render the Deployment", func() {
			rawObject, err := templates.Get(testTemplates, "test3.yml.tpl", false, struct{ Name string }{Name: "tres"})
			Expect(err).ToNot(HaveOccurred())
			deployment, ok := rawObject.(*appsv1.Deployment)
			Expect(ok).To(BeTrue())
			Expect(deployment.Name).To(Equal("test-tres"))
			Expect(deployment.Spec.Replicas).To(PointTo(BeEquivalentTo(1)))
		})
	})

	Context("unstructured mode", func() {
		It("should render the Deployment", func() {
			rawObject, err := templates.Get(testTemplates, "test1.yml.tpl", true, struct{}{})
			Expect(err).ToNot(HaveOccurred())
			_, ok := rawObject.(*appsv1.Deployment)
			Expect(ok).To(BeFalse())
			obj, ok := rawObject.(*unstructured.Unstructured)
			Expect(ok).To(BeTrue())
			Expect(obj.GetAPIVersion()).To(Equal("apps/v1"))
			Expect(obj.GetKind()).To(Equal("Deployment"))
			Expect(obj.GetName()).To(Equal("test"))
			Expect(obj.GetNamespace()).To(Equal("default"))
		})
	})
})
