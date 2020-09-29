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
	"reflect"

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
