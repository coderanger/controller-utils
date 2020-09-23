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

package templates_test

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	// . "github.com/onsi/gomega/gstruct"

	"github.com/coderanger/controller-utils/templates"
)

var testFilteredTemplates *templates.FilteredFileSystem = templates.NewFilteredFileSystem(http.Dir("test_templates"))

var _ = Describe("FilteredFileSystem", func() {
	Context("no filters", func() {
		It("lists all files", func() {
			f, err := testFilteredTemplates.Open("/")
			Expect(err).ToNot(HaveOccurred())
			fis, err := f.Readdir(0)
			Expect(err).ToNot(HaveOccurred())
			Expect(fis).To(HaveLen(5))
		})

		It("can read test.txt", func() {
			_, err := testFilteredTemplates.Open("/test.txt")
			Expect(err).ToNot(HaveOccurred())
		})

		It("can read test1.yml.tpl", func() {
			_, err := testFilteredTemplates.Open("/test1.yml.tpl")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("an exclude filter", func() {
		exclude := testFilteredTemplates.Exclude("*.txt")

		It("lists the correct files", func() {
			f, err := exclude.Open("/")
			Expect(err).ToNot(HaveOccurred())
			fis, err := f.Readdir(0)
			Expect(err).ToNot(HaveOccurred())
			Expect(fis).To(HaveLen(4))
		})

		It("cannot read test.txt", func() {
			_, err := exclude.Open("/test.txt")
			Expect(err).To(HaveOccurred())
		})

		It("can read test1.yml.tpl", func() {
			_, err := exclude.Open("/test1.yml.tpl")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("an include filter", func() {
		include := testFilteredTemplates.Include("*.txt")

		It("lists the correct files", func() {
			_, err := include.Open("/")
			Expect(err).To(HaveOccurred())
		})

		It("can read test.txt", func() {
			_, err := include.Open("/test.txt")
			Expect(err).ToNot(HaveOccurred())
		})

		It("cannot read test1.yml.tpl", func() {
			_, err := include.Open("/test1.yml.tpl")
			Expect(err).To(HaveOccurred())
		})
	})
})
