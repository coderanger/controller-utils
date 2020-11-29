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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/coderanger/controller-utils/conditions"
	"github.com/coderanger/controller-utils/tests"
)

var _ = Describe("ReadyStatus component", func() {
	var helper *tests.FunctionalHelper
	var obj *TestObject

	setCondition := func(conditionType string, status metav1.ConditionStatus) {
		condition := conditions.Condition{
			Type:               conditionType,
			Status:             status,
			ObservedGeneration: obj.Generation,
			Reason:             "Fake",
		}
		conditions.SetStatusCondition(&obj.Status.Conditions, condition)
	}

	BeforeEach(func() {
		obj = &TestObject{
			ObjectMeta: metav1.ObjectMeta{Name: "testing"},
		}
	})

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			helper.DebugList(&TestObjectList{})
		}
		if helper != nil {
			helper.MustStop()
		}
		helper = nil
	})

	It("sets a ready status with no keys", func() {
		comp := NewReadyStatusComponent()
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)
		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "True"))
	})

	It("sets a ready status with one key", func() {
		comp := NewReadyStatusComponent("One")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)
		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))

		// Set things to True.
		objClean := obj.DeepCopy()
		setCondition("One", metav1.ConditionTrue)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "True"))

		// Set them back to False.
		objClean = obj.DeepCopy()
		setCondition("One", metav1.ConditionFalse)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))
	})

	It("sets a ready status with two keys", func() {
		comp := NewReadyStatusComponent("One", "Two")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)
		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))

		// Set things to True.
		objClean := obj.DeepCopy()
		setCondition("One", metav1.ConditionTrue)
		setCondition("Two", metav1.ConditionTrue)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "True"))

		// Set one back to False.
		objClean = obj.DeepCopy()
		setCondition("Two", metav1.ConditionFalse)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))
		cond := conditions.FindStatusCondition(obj.Status.Conditions, "Ready")
		Expect(cond.Message).To(Equal("ReadyStatusComponent did not observe correct status of Two"))
	})

	It("handles negative polarity", func() {
		comp := NewReadyStatusComponent("One", "-Two")
		helper = startTestController(comp)
		c := helper.TestClient

		c.Create(obj)
		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))

		// Set things to True.
		objClean := obj.DeepCopy()
		setCondition("One", metav1.ConditionTrue)
		setCondition("Two", metav1.ConditionFalse)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "True"))

		// Set them back to False.
		objClean = obj.DeepCopy()
		setCondition("Two", metav1.ConditionTrue)
		c.Status().Patch(obj, client.MergeFrom(objClean))

		c.EventuallyGetName(obj.Name, obj, c.EventuallyCondition("Ready", "False"))
	})
})
