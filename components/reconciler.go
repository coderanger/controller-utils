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
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Supporting mocking out functions for testing
var getGvk = apiutil.GVKForObject

type Reconciler struct {
	name              string
	mgr               ctrl.Manager
	controllerBuilder *ctrl.Builder
	controller        controller.Controller
	apiType           runtime.Object
	components        []reconcilerComponent
	log               logr.Logger
	client            client.Client
	templates         http.FileSystem
}

// Concrete component instance.
type reconcilerComponent struct {
	name string
	comp Component
}

func NewReconciler(mgr ctrl.Manager) *Reconciler {
	return &Reconciler{
		mgr:               mgr,
		controllerBuilder: builder.ControllerManagedBy(mgr),
		components:        []reconcilerComponent{},
		client:            mgr.GetClient(),
	}
}

func (r *Reconciler) For(apiType runtime.Object) *Reconciler {
	r.apiType = apiType
	r.controllerBuilder = r.controllerBuilder.For(apiType)
	return r
}

func (r *Reconciler) Component(name string, comp Component) *Reconciler {
	r.components = append(r.components, reconcilerComponent{name: name, comp: comp})
	return r
}

func (r *Reconciler) TemplateComponent(template string) *Reconciler {
	name := template[strings.LastIndex(template, ".")+1:]
	return r.Component(name, NewTemplateComponent(template))
}

func (r *Reconciler) Templates(t http.FileSystem) *Reconciler {
	r.templates = t
	return r
}

func (r *Reconciler) Complete() error {
	_, err := r.Build()
	return err
}

func (r *Reconciler) getControllerName() (string, error) {
	if r.name != "" {
		return r.name, nil
	}
	gvk, err := getGvk(r.apiType, r.mgr.GetScheme())
	if err != nil {
		return "", err
	}
	return strings.ToLower(gvk.Kind), nil
}

func (r *Reconciler) Build() (controller.Controller, error) {
	name, err := r.getControllerName()
	if err != nil {
		return nil, errors.Wrap(err, "error computing controller name")
	}
	r.name = name
	r.log = ctrl.Log.WithName("controllers").WithName(name)

	setupCtx := &Context{
		Context:   context.Background(),
		Client:    r.client,
		Templates: r.templates,
		Scheme:    r.mgr.GetScheme(),
		Object:    r.apiType.DeepCopyObject(),
	}
	// Provide some bare minimum data
	setupObj := setupCtx.Object.(metav1.Object)
	setupObj.SetName("setup")
	setupObj.SetNamespace("setup")
	log := r.log.WithName("components")
	for _, rc := range r.components {
		setupCtx.Log = log.WithName(rc.name)
		setupCtx.FieldManager = fmt.Sprintf("%s/%s", r.name, rc.name)
		err := rc.comp.Setup(setupCtx, r.controllerBuilder)
		if err != nil {
			return nil, errors.Wrapf(err, "error initializing component %s in controller %s", rc.name, r.name)
		}
	}
	controller, err := r.controllerBuilder.Build(r)
	if err != nil {
		return nil, errors.Wrapf(err, "error building controller %s", r.name)
	}
	r.controller = controller
	return controller, nil
}

func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("object", req)
	log.Info("Starting reconcile")

	ctx := &Context{
		Context:   context.Background(),
		Client:    r.client,
		Templates: r.templates,
		Scheme:    r.mgr.GetScheme(),
	}

	obj := r.apiType.DeepCopyObject()
	err := r.client.Get(ctx, req.NamespacedName, obj)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Object not found, likely already deleted, just silenty bail.
			log.Info("Aborting reconcile, object already deleted")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, errors.Wrap(err, "error getting reconcile object")
	}
	ctx.Object = obj
	cleanObj := obj.DeepCopyObject()

	// Check for annotation that blocks reconciles, exit early if found.
	metaObj := obj.(metav1.Object)
	annotations := metaObj.GetAnnotations()
	reconcileBlocked, ok := annotations["controller-utils/skip-reconcile"]
	if ok && reconcileBlocked == "true" {
		log.Info("Skipping reconcile due to annotation")
		return reconcile.Result{}, nil
	}

	// Reconcile the components.
	compLog := log.WithName("components")
	for _, rc := range r.components {
		// Create the per-component logger.
		ctx.Log = compLog.WithName(rc.name)
		ctx.FieldManager = fmt.Sprintf("%s/%s", r.name, rc.name)
		res, err := rc.comp.Reconcile(ctx)
		ctx.mergeResult(res, err)
		if err != nil {
			log.Error(err, "error in component reconcile", "component", rc.name)
			return ctx.result, errors.Wrapf(err, "error in %s component reconcile", rc.name)
		}
	}

	// Save the object status.
	err = r.client.Status().Patch(ctx, ctx.Object, client.MergeFrom(cleanObj), &client.PatchOptions{FieldManager: r.name})
	if err != nil {
		return ctx.result, errors.Wrap(err, "error applying status")
	}

	return ctx.result, nil
}
