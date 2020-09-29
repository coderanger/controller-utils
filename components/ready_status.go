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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/coderanger/controller-utils/conditions"
	"github.com/coderanger/controller-utils/core"
	"github.com/pkg/errors"
)

type readyStatusComponent struct {
	keys            []string
	readyConditions map[string]metav1.ConditionStatus
}

// Create a ReadyStatus component. Takes 0 or more conditions types. If a type
// name starts with `-` then the goal is for it to be false, otherwise true. If
// all requested conditions match, the Ready condition will be set to True.
func NewReadyStatusComponent(keys ...string) core.Component {
	// Parse the keys to a map of the desired statuses so it's faster to check them in the Reconcile.
	readyConditions := map[string]metav1.ConditionStatus{}
	for _, key := range keys {
		target := metav1.ConditionTrue
		if strings.HasPrefix(key, "-") {
			key = key[1:]
			target = metav1.ConditionFalse
		}
		readyConditions[key] = target
	}
	return &readyStatusComponent{keys: keys, readyConditions: readyConditions}
}

func (comp *readyStatusComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	metaObj := ctx.Object.(metav1.Object)
	objConditions, err := core.GetConditionsFor(ctx.Object)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error getting object conditions")
	}
	readyCondition := conditions.Condition{
		Type:               "Ready", // TODO This should be configurable somehow.
		Status:             metav1.ConditionTrue,
		ObservedGeneration: metaObj.GetGeneration(),
		Reason:             "CompositeReady",
		Message:            fmt.Sprintf("ReadyStatusComponent observed correct status of %s", strings.Join(comp.keys, ", ")),
	}
	for conditionType, status := range comp.readyConditions {
		if !conditions.IsStatusConditionPresentAndEqual(*objConditions, conditionType, status) {
			readyCondition.Status = metav1.ConditionFalse
			break
		}
	}
	conditions.SetStatusCondition(objConditions, readyCondition)
	return core.Result{}, nil
}

func init() {
	// Avoid import loops.
	core.NewReadyStatusComponent = NewReadyStatusComponent
}
