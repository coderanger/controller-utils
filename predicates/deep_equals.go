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

package predicates

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Predicate that uses DeepEquals to work around https://github.com/kubernetes/kubernetes/issues/95460.
type deepEqualsPredicate struct{}

func DeepEquals() *deepEqualsPredicate {
	return &deepEqualsPredicate{}
}

var _ predicate.Predicate = &deepEqualsPredicate{}

// Create returns true if the Create event should be processed
func (_ *deepEqualsPredicate) Create(_ event.CreateEvent) bool {
	return true
}

// Delete returns true if the Delete event should be processed
func (_ *deepEqualsPredicate) Delete(_ event.DeleteEvent) bool {
	return true
}

// Update returns true if the Update event should be processed
func (_ *deepEqualsPredicate) Update(evt event.UpdateEvent) bool {
	cleanOld := evt.ObjectOld.DeepCopyObject().(metav1.Object)
	cleanNew := evt.ObjectNew.DeepCopyObject().(metav1.Object)
	cleanOld.SetGeneration(0)
	cleanNew.SetGeneration(0)
	cleanOld.SetResourceVersion("")
	cleanNew.SetResourceVersion("")
	cleanOld.SetManagedFields([]metav1.ManagedFieldsEntry{})
	cleanNew.SetManagedFields([]metav1.ManagedFieldsEntry{})
	return !reflect.DeepEqual(cleanNew, cleanOld)
}

// Generic returns true if the Generic event should be processed
func (_ *deepEqualsPredicate) Generic(_ event.GenericEvent) bool {
	return true
}
