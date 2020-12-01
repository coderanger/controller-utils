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
	"fmt"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var updateDebugLog = logf.Log.WithName("predicates").WithName("UpdateDebug")

type updateDebugPredicate struct{}

func UpdateDebug() *updateDebugPredicate {
	return &updateDebugPredicate{}
}

var _ predicate.Predicate = &updateDebugPredicate{}

// Create returns true if the Create event should be processed
func (_ *updateDebugPredicate) Create(_ event.CreateEvent) bool {
	return true
}

// Delete returns true if the Delete event should be processed
func (_ *updateDebugPredicate) Delete(_ event.DeleteEvent) bool {
	return true
}

// Update returns true if the Update event should be processed
func (_ *updateDebugPredicate) Update(evt event.UpdateEvent) bool {
	if os.Getenv("DEBUG_UPDATE") == "true" {
		obj := fmt.Sprintf("%s/%s", evt.MetaNew.GetNamespace(), evt.MetaNew.GetName())
		diff, err := client.MergeFrom(evt.ObjectOld).Data(evt.ObjectNew)
		if err != nil {
			updateDebugLog.Info("error generating diff", "err", err, "obj", obj)
		} else {
			updateDebugLog.Info("Update diff", "diff", string(diff), "obj", obj)
		}
	}
	return true
}

// Generic returns true if the Generic event should be processed
func (_ *updateDebugPredicate) Generic(_ event.GenericEvent) bool {
	return true
}
