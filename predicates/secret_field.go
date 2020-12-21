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
	"bytes"
	"encoding/base64"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type secretFieldPredicate struct {
	keys []string
}

func SecretField(keys []string) *secretFieldPredicate {
	return &secretFieldPredicate{keys: keys}
}

var _ predicate.Predicate = &secretFieldPredicate{}

// Create returns true if the Create event should be processed
func (_ *secretFieldPredicate) Create(_ event.CreateEvent) bool {
	return true
}

// Delete returns true if the Delete event should be processed
func (_ *secretFieldPredicate) Delete(_ event.DeleteEvent) bool {
	return true
}

// Update returns true if the Update event should be processed
func (p *secretFieldPredicate) Update(evt event.UpdateEvent) bool {
	oldData, ok := p.secretData(evt.ObjectOld)
	if !ok {
		return true
	}
	newData, ok := p.secretData(evt.ObjectNew)
	if !ok {
		return true
	}
	for _, key := range p.keys {
		oldVal, oldOk := oldData[key]
		newVal, newOk := newData[key]
		if oldOk != newOk || !bytes.Equal(oldVal, newVal) {
			return true
		}
	}
	return false
}

// Generic returns true if the Generic event should be processed
func (_ *secretFieldPredicate) Generic(_ event.GenericEvent) bool {
	return true
}

func (_ *secretFieldPredicate) secretData(obj runtime.Object) (map[string][]byte, bool) {
	secret, ok := obj.(*corev1.Secret)
	if ok {
		return secret.Data, true
	}
	unstructured, ok := obj.(*unstructured.Unstructured)
	if ok {
		gvk := obj.GetObjectKind().GroupVersionKind()
		if gvk.Group == "" && gvk.Kind == "Secret" {
			data, ok := unstructured.UnstructuredContent()["data"]
			if ok {
				// Because unstructured skips the base64 decode, we have to do that now.
				cleanData := map[string][]byte{}
				for k, v := range data.(map[string]interface{}) {
					cleanV, err := base64.StdEncoding.DecodeString(v.(string))
					if err != nil {
						// kube-apiserver sent us corrupted data, fuck it.
						panic(err)
					}
					cleanData[k] = cleanV
				}
				return cleanData, true
			}
		}
	}
	return nil, false
}
