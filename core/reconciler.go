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
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/coderanger/controller-utils/conditions"
)

// Supporting mocking out functions for testing
var getGvk = apiutil.GVKForObject

// Avoid an import loop. Sighs in Go.
var NewRandomSecretComponent func(string, ...string) Component
var NewReadyStatusComponent func(...string) Component
var NewTemplateComponent func(string, func(runtime.Object) conditions.Condition) Component

type Reconciler struct {
	name              string
	mgr               ctrl.Manager
	controllerBuilder *ctrl.Builder
	controller        controller.Controller
	apiType           runtime.Object
	components        []reconcilerComponent
	log               logr.Logger
	client            client.Client
	uncachedClient    client.Client
	templates         http.FileSystem
	events            record.EventRecorder
}

// Concrete component instance.
type reconcilerComponent struct {
	name string
	comp Component
}

func NewReconciler(mgr ctrl.Manager) *Reconciler {
	rawClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		panic(err)
	}
	return &Reconciler{
		mgr:               mgr,
		controllerBuilder: builder.ControllerManagedBy(mgr),
		components:        []reconcilerComponent{},
		client:            mgr.GetClient(),
		uncachedClient:    rawClient,
	}
}

func (r *Reconciler) For(apiType runtime.Object) *Reconciler {
	r.apiType = apiType
	r.controllerBuilder = r.controllerBuilder.For(apiType)
	return r
}

func (r *Reconciler) Templates(t http.FileSystem) *Reconciler {
	r.templates = t
	return r
}

func (r *Reconciler) Component(name string, comp Component) *Reconciler {
	r.components = append(r.components, reconcilerComponent{name: name, comp: comp})
	return r
}

func (r *Reconciler) TemplateComponent(template string, condGetter func(runtime.Object) conditions.Condition) *Reconciler {
	name := template[strings.LastIndex(template, ".")+1:]
	return r.Component(name, NewTemplateComponent(template, condGetter))
}

func (r *Reconciler) RandomSecretComponent(name string, keys ...string) *Reconciler {
	// TODO This is super awkward. Maybe just provisionally set r.name from For()?
	controllerName, err := r.getControllerName()
	if err != nil {
		panic(err)
	}
	nameTemplate := fmt.Sprintf("%%s-%s-%s", controllerName, name)
	return r.Component(name, NewRandomSecretComponent(nameTemplate, keys...))
}

func (r *Reconciler) ReadyStatusComponent(keys ...string) *Reconciler {
	return r.Component("readyStatus", NewReadyStatusComponent(keys...))
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
		Context:        context.Background(),
		Client:         r.client,
		UncachedClient: r.uncachedClient,
		Templates:      r.templates,
		Scheme:         r.mgr.GetScheme(),
		Object:         r.apiType.DeepCopyObject(),
	}
	// Provide some bare minimum data
	setupObj := setupCtx.Object.(metav1.Object)
	setupObj.SetName("setup")
	setupObj.SetNamespace("setup")
	log := r.log.WithName("components")
	for _, rc := range r.components {
		setupComp, ok := rc.comp.(ComponentSetup)
		if !ok {
			continue
		}
		setupCtx.Log = log.WithName(rc.name)
		setupCtx.FieldManager = fmt.Sprintf("%s/%s", r.name, rc.name)
		err := setupComp.Setup(setupCtx, r.controllerBuilder)
		if err != nil {
			return nil, errors.Wrapf(err, "error initializing component %s in controller %s", rc.name, r.name)
		}
	}
	controller, err := r.controllerBuilder.Build(r)
	if err != nil {
		return nil, errors.Wrapf(err, "error building controller %s", r.name)
	}
	r.controller = controller
	r.events = r.mgr.GetEventRecorderFor(r.name + "-controller")
	return controller, nil
}

func (r *Reconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	log := r.log.WithValues("object", req)
	log.Info("Starting reconcile")

	ctx := &Context{
		Context:        context.Background(),
		Client:         r.client,
		UncachedClient: r.uncachedClient,
		Templates:      r.templates,
		Scheme:         r.mgr.GetScheme(),
		Events:         r.events,
		Data:           ContextData{},
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
	ctx.Conditions = NewConditionsHelper(obj)
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
		err = ctx.mergeResult(res, err)
		if err != nil {
			log.Error(err, "error in component reconcile", "component", rc.name)
			return ctx.result, errors.Wrapf(err, "error in %s component reconcile", rc.name)
		}
		if res.SkipRemaining {
			// Abort reconcile to skip remaining components.
			break
		}
	}

	// Save the object status.
	err = r.client.Status().Patch(ctx, ctx.Object, client.MergeFrom(cleanObj), &client.PatchOptions{FieldManager: r.name})
	if err != nil && !kerrors.IsNotFound(err) {
		// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
		return ctx.result, errors.Wrap(err, "error patching status")
	}

	return ctx.result, nil
}
