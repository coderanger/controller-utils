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
	"strings"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/predicates"
	"github.com/coderanger/controller-utils/templates"
)

const DELETE_ANNOTATION = "controller-utils/delete"
const CONDITION_ANNOTATION = "controller-utils/condition"
const DEEPEQUALS_ANNOTATION = "controller-utils/deepEquals"
const SECRETFIELD_ANNOTATION = "controller-utils/secretField"

type templateComponent struct {
	template      string
	conditionType string
}

type templateData struct {
	Object core.Object
	Data   map[string]interface{}
}

func NewTemplateComponent(template string, conditionType string) core.Component {
	return &templateComponent{template: template, conditionType: conditionType}
}

func (comp *templateComponent) GetReadyCondition() string {
	return comp.conditionType
}

func (comp *templateComponent) Setup(ctx *core.Context, bldr *ctrl.Builder) error {
	// Render with a fake, blank object just to find the object type.
	obj, err := comp.renderTemplate(ctx, true)
	if err != nil {
		return errors.Wrap(err, "error rendering setup template")
	}
	// Check if we should use the slower DeepEquals predicate.
	annotations := obj.GetAnnotations()
	deepEquals, ok := annotations[DEEPEQUALS_ANNOTATION]
	secretField, ok2 := annotations[SECRETFIELD_ANNOTATION]
	if ok && deepEquals == "true" {
		bldr.Owns(obj, builder.WithPredicates(predicates.DeepEquals()))
	} else if ok2 && secretField != "" {
		bldr.Owns(obj, builder.WithPredicates(predicates.SecretField(strings.Split(secretField, ","))))
	} else {
		bldr.Owns(obj)
	}
	return nil
}

func (comp *templateComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	// Render the object to an Unstructured.
	obj, err := comp.renderTemplate(ctx, true)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error rendering template")
	}

	// Default the namespace to the controlling object namespace.
	if obj.GetNamespace() == "" {
		obj.SetNamespace(ctx.Object.(metav1.Object).GetNamespace())
	}

	// Check for delete annotation.
	annotations := obj.GetAnnotations()
	shouldDelete, ok := annotations[DELETE_ANNOTATION]
	if ok {
		delete(annotations, DELETE_ANNOTATION)
		obj.SetAnnotations(annotations)
	} else {
		shouldDelete = "false"
	}

	// Hide the DeepEquals annotation.
	_, ok = annotations[DEEPEQUALS_ANNOTATION]
	if ok {
		delete(annotations, DEEPEQUALS_ANNOTATION)
		obj.SetAnnotations(annotations)
	}

	if shouldDelete == "true" {
		return comp.reconcileDelete(ctx, obj)
	} else {
		return comp.reconcileCreate(ctx, obj)
	}
}

func (comp *templateComponent) renderTemplate(ctx *core.Context, unstructured bool) (core.Object, error) {
	return templates.Get(ctx.Templates, comp.template, unstructured, templateData{Object: ctx.Object, Data: ctx.Data})
}

func (comp *templateComponent) reconcileCreate(ctx *core.Context, obj core.Object) (core.Result, error) {
	// Set owner reference.
	err := controllerutil.SetControllerReference(ctx.Object, obj, ctx.Scheme)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error setting owner reference")
	}

	// Apply the object data.
	force := true // Sigh *bool.
	err = ctx.Client.Patch(ctx, obj, client.Apply, &client.PatchOptions{Force: &force, FieldManager: ctx.FieldManager})
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error applying object")
	}

	// If we have a condition setter, check on the object status.
	if comp.conditionType != "" {
		currentObj := &unstructured.Unstructured{}
		currentObj.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
		err = ctx.Client.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, currentObj)
		if err != nil {
			return core.Result{}, errors.Wrapf(err, "error getting current object %s/%s for status", obj.GetNamespace(), obj.GetName())
		}

		annotations := obj.GetAnnotations()
		if val, ok := annotations[CONDITION_ANNOTATION]; ok {
			status, ok := comp.getStatusFromUnstructured(currentObj, val)
			if ok {
				ctx.Conditions.Setf(comp.conditionType, status, "UpstreamConditionSet", "Upstream condition %s on %s %s was set to %s", val, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), status)
			} else {
				ctx.Conditions.SetfUnknown(comp.conditionType, "UpstreamConditionNotSet", "Upstream condition %s on %s %s was not set", val, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
			}
		}
		// TODO some kind of support for an expr or CEL based option to get a status for upstream objects that don't use status conditions.
	}

	return core.Result{}, nil
}

func (comp *templateComponent) reconcileDelete(ctx *core.Context, obj core.Object) (core.Result, error) {
	currentObj := &unstructured.Unstructured{}
	currentObj.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
	err := ctx.Client.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, currentObj)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Didn't exist at all so we're good.
			if comp.conditionType != "" {
				ctx.Conditions.SetfTrue(comp.conditionType, "UpstreamDoesNotExist", "Upstream %s %s does not exist", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
			}
			return core.Result{}, nil
		}
		return core.Result{}, errors.Wrapf(err, "error getting current object %s/%s for owner", obj.GetNamespace(), obj.GetName())
	}
	controllerRef := metav1.GetControllerOf(currentObj)
	if controllerRef == nil || !comp.referSameObject(controllerRef, ctx.Object, ctx.Scheme) {
		// The object exists but isn't owned by this object so don't purge it.
		if comp.conditionType != "" {
			ctx.Conditions.SetfTrue(comp.conditionType, "UpstreamNotOwned", "Upstream %s %s is not owned by %s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), ctx.Object.GetName())
		}
		return core.Result{}, nil
	}

	propagation := metav1.DeletePropagationBackground
	err = ctx.Client.Delete(ctx, obj, &client.DeleteOptions{PropagationPolicy: &propagation})
	if err != nil && !kerrors.IsNotFound(err) {
		return core.Result{}, errors.Wrapf(err, "error deleting %s/%s", obj.GetNamespace(), obj.GetName())
	}
	if comp.conditionType != "" {
		ctx.Conditions.SetfTrue(comp.conditionType, "UpstreamDoesNotExist", "Upstream %s %s does not exist", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
	}
	return core.Result{}, nil
}

func (comp *templateComponent) getStatusFromUnstructured(obj runtime.Object, srcType string) (metav1.ConditionStatus, bool) {
	data := obj.(*unstructured.Unstructured).UnstructuredContent()

	// Ooof this is ugly. Once I am set up with Expr or CEL or even a JSONPath library, try and use that instead.
	maybeStatus, ok := data["status"]
	if !ok {
		return metav1.ConditionUnknown, false
	}

	status, ok := maybeStatus.(map[string]interface{})
	if !ok {
		return metav1.ConditionUnknown, false
	}

	maybeConditions, ok := status["conditions"]
	if !ok {
		return metav1.ConditionUnknown, false
	}

	conditions_, ok := maybeConditions.([]interface{})
	if !ok {
		return metav1.ConditionUnknown, false
	}

	var status_ string
	for _, maybeCondition := range conditions_ {
		if condition_, ok := maybeCondition.(map[string]interface{}); ok {
			maybeType, ok := condition_["type"]
			maybeStatus, ok2 := condition_["status"]
			if ok && ok2 {
				type_, ok := maybeType.(string)
				status_, ok2 = maybeStatus.(string)
				if ok && ok2 && srcType == type_ {
					if status_ == "True" {
						return metav1.ConditionTrue, true
					} else if status_ == "False" {
						return metav1.ConditionFalse, true
					} else if status_ == "Unknown" {
						return metav1.ConditionUnknown, true
					}
				}
			}
		}
	}

	// Wasn't in there, we tried.
	return metav1.ConditionUnknown, false
}

// Adapted from controller-runtime.
// Copyright 2018 The Kubernetes Authors.
func (comp *templateComponent) referSameObject(ownerRef *metav1.OwnerReference, obj core.Object, scheme *runtime.Scheme) bool {
	ownerGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
	if err != nil {
		return false
	}

	objGVK, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return false
	}

	return ownerGV.Group == objGVK.Group && ownerRef.Kind == objGVK.Kind && ownerRef.Name == obj.GetName()
}

func init() {
	// Avoid import loops.
	core.NewTemplateComponent = NewTemplateComponent
}
