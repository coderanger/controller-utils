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
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/templates"
)

type templateComponent struct {
	template string
}

func NewTemplateComponent(template string) core.Component {
	return &templateComponent{template}
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
	obj, err := comp.renderTemplate(ctx, true)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error rendering template")
	}
	err = controllerutil.SetControllerReference(ctx.Object.(metav1.Object), obj.(metav1.Object), ctx.Scheme)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error setting owner reference")
	}
	// Sigh *bool.
	force := true
	err = ctx.Client.Patch(ctx, obj, client.Apply, &client.PatchOptions{Force: &force, FieldManager: ctx.FieldManager})
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error applying object")
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
