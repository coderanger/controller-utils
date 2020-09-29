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

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/coderanger/controller-utils/conditions"
	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/templates"
)

type conditionGetter = func(runtime.Object) conditions.Condition

type templateComponent struct {
	template string
	getter   conditionGetter
}

func NewTemplateComponent(template string, getter conditionGetter) core.Component {
	return &templateComponent{template: template, getter: getter}
}

func TemplateConditionGetter(destType, srcType string) conditionGetter {
	return func(obj runtime.Object) conditions.Condition {
		metaObj := obj.(metav1.Object)
		objKind := obj.(schema.ObjectKind)
		data := obj.(*unstructured.Unstructured).UnstructuredContent()
		condition := conditions.Condition{
			Type:    destType,
			Status:  metav1.ConditionUnknown,
			Reason:  "UpsteamConditionNotSet",
			Message: fmt.Sprintf("Upstream condition %s on %s %s was not set", srcType, objKind.GroupVersionKind().Kind, metaObj.GetName()),
		}

		// Ooof this is ugly.
		maybeStatus, ok := data["status"]
		if !ok {
			return condition
		}

		status, ok := maybeStatus.(map[string]interface{})
		if !ok {
			return condition
		}

		maybeConditions, ok := status["conditions"]
		if !ok {
			return condition
		}

		conditions_, ok := maybeConditions.([]interface{})
		if !ok {
			return condition
		}

		var status_ string
		foundCondition := false
		for _, maybeCondition := range conditions_ {
			if condition_, ok := maybeCondition.(map[string]interface{}); ok {
				maybeType, ok := condition_["type"]
				maybeStatus, ok2 := condition_["status"]
				if ok && ok2 {
					type_, ok := maybeType.(string)
					status_, ok2 = maybeStatus.(string)
					if ok && ok2 && srcType == type_ {
						foundCondition = true
						if status_ == "True" {
							condition.Status = metav1.ConditionTrue
						} else if status_ == "False" {
							condition.Status = metav1.ConditionFalse
						}
					}
				}
			}
		}

		// Phew the ugly part is over.
		if !foundCondition {
			return condition
		}

		// Fix the reason and message.
		condition.Reason = "UpstreamConditionSet"
		condition.Message = fmt.Sprintf("Upstream condition %s on %s %s was set to %s", srcType, objKind.GroupVersionKind().Kind, metaObj.GetName(), status_)
		return condition
	}
}

func (comp *templateComponent) Setup(ctx *core.Context, bldr *ctrl.Builder) error {
	// Render with a fake, blank object just to find the object type.
	obj, err := comp.renderTemplate(ctx, true)
	if err != nil {
		return errors.Wrap(err, "error rendering setup template")
	}
	bldr.Owns(obj)
	return nil
}

func (comp *templateComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	// Render the object to an Unstructured.
	obj, err := comp.renderTemplate(ctx, true)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error rendering template")
	}

	// Default the namespace to the controlling object namespace.
	metaObj := obj.(metav1.Object)
	if metaObj.GetNamespace() == "" {
		metaObj.SetNamespace(ctx.Object.(metav1.Object).GetNamespace())
	}

	// Set owner reference.
	err = controllerutil.SetControllerReference(ctx.Object.(metav1.Object), obj.(metav1.Object), ctx.Scheme)
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
	if comp.getter != nil {
		objConditions, err := core.GetConditionsFor(ctx.Object)
		if err != nil {
			return core.Result{}, errors.Wrap(err, "error getting object conditions")
		}
		currentObj := obj.DeepCopyObject()
		err = ctx.Client.Get(ctx, types.NamespacedName{Name: metaObj.GetName(), Namespace: metaObj.GetNamespace()}, currentObj)
		if err != nil {
			return core.Result{}, errors.Wrapf(err, "error getting current object %s/%s for status", metaObj.GetNamespace(), metaObj.GetName())
		}
		condition := comp.getter(currentObj)
		condition.ObservedGeneration = ctx.Object.(metav1.Object).GetGeneration()
		conditions.SetStatusCondition(objConditions, condition)
	}

	return core.Result{}, nil
}

func (comp *templateComponent) renderTemplate(ctx *core.Context, unstructured bool) (runtime.Object, error) {
	templateData := struct{ Object runtime.Object }{Object: ctx.Object}
	return templates.Get(ctx.Templates, comp.template, unstructured, templateData)
}

func init() {
	// Avoid import loops.
	core.NewTemplateComponent = NewTemplateComponent
}
