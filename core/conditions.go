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

package core

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/coderanger/controller-utils/conditions"
)

type ConditionsObject interface {
	GetConditions() *[]conditions.Condition
}

func GetConditionsFor(obj runtime.Object) (*[]conditions.Condition, error) {
	// Try the simple and correct way.
	condObj, ok := obj.(ConditionsObject)
	if ok {
		return condObj.GetConditions(), nil
	}

	// Supply a dynamic fallback until I can get some code generation in place.
	// Yes, I know this code is awful.
	statusVal := reflect.ValueOf(obj).FieldByName("Status")
	if statusVal.IsValid() {
		conditionsVal := statusVal.FieldByName("Conditions")
		if conditionsVal.IsValid() {
			maybeConditions := conditionsVal.Addr().Interface()
			conditions, ok := maybeConditions.(*[]conditions.Condition)
			if ok {
				return conditions, nil
			}
		}
	}

	return nil, errors.New("unable to get conditions")
}

type conditionsHelper struct {
	obj runtime.Object
}

func (h *conditionsHelper) Set(conditionType string, status metav1.ConditionStatus, reason string, message ...string) {
	metaObj := h.obj.(metav1.Object)
	conds, err := GetConditionsFor(h.obj)
	if err != nil {
		// This should be a 100% static error so just panic.
		panic(err)
	}
	conditions.SetStatusCondition(conds, conditions.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: metaObj.GetGeneration(),
		Reason:             reason,
		Message:            strings.Join(message, ""),
	})
}

func (h *conditionsHelper) Setf(conditionType string, status metav1.ConditionStatus, reason string, message string, args ...interface{}) {
	h.Set(conditionType, status, reason, fmt.Sprintf(message, args...))
}

func (h *conditionsHelper) SetTrue(conditionType string, reason string, message ...string) {
	h.Set(conditionType, metav1.ConditionTrue, reason, message...)
}

func (h *conditionsHelper) SetfTrue(conditionType string, reason string, message string, args ...interface{}) {
	h.Setf(conditionType, metav1.ConditionTrue, reason, message, args...)
}

func (h *conditionsHelper) SetFalse(conditionType string, reason string, message ...string) {
	h.Set(conditionType, metav1.ConditionFalse, reason, message...)
}

func (h *conditionsHelper) SetfFalse(conditionType string, reason string, message string, args ...interface{}) {
	h.Setf(conditionType, metav1.ConditionFalse, reason, message, args...)
}

func (h *conditionsHelper) SetUnknown(conditionType string, reason string, message ...string) {
	h.Set(conditionType, metav1.ConditionUnknown, reason, message...)
}

func (h *conditionsHelper) SetfUnknown(conditionType string, reason string, message string, args ...interface{}) {
	h.Setf(conditionType, metav1.ConditionUnknown, reason, message, args...)
}
