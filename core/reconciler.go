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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Supporting mocking out functions for testing
var getGvk = apiutil.GVKForObject

// Avoid an import loop. Sighs in Go.
var NewRandomSecretComponent func(string, ...string) Component
var NewReadyStatusComponent func(...string) Component
var NewTemplateComponent func(string, string) Component

type Reconciler struct {
	name              string
	mgr               ctrl.Manager
	controllerBuilder *ctrl.Builder
	controller        controller.Controller
	apiType           runtime.Object
	components        []*reconcilerComponent
	log               logr.Logger
	client            client.Client
	uncachedClient    client.Client
	templates         http.FileSystem
	events            record.EventRecorder
	webhook           bool
	finalizerBaseName string
}

// Concrete component instance.
type reconcilerComponent struct {
	name string
	comp Component
	// Same component as comp but as a finalizer if possible, otherwise nil.
	finalizer     FinalizerComponent
	finalizerName string
	// Tracking data for status conditions.
	readyCondition       string
	errorConditionStatus metav1.ConditionStatus
}

func NewReconciler(mgr ctrl.Manager) *Reconciler {
	rawClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		panic(err)
	}
	return &Reconciler{
		mgr:               mgr,
		controllerBuilder: builder.ControllerManagedBy(mgr),
		components:        []*reconcilerComponent{},
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

func (r *Reconciler) Webhook() *Reconciler {
	r.webhook = true
	return r
}

func (r *Reconciler) Component(name string, comp Component) *Reconciler {
	rc := &reconcilerComponent{name: name, comp: comp}
	finalizer, ok := comp.(FinalizerComponent)
	if ok {
		rc.finalizer = finalizer
	}
	readyCond, ok := comp.(ReadyConditionComponent)
	if ok {
		rc.readyCondition = readyCond.GetReadyCondition()
		rc.errorConditionStatus = metav1.ConditionFalse
		// If the first character is !, trim it off and invert the error status.
		if rc.readyCondition != "" && rc.readyCondition[0] == '!' {
			rc.readyCondition = rc.readyCondition[1:]
			rc.errorConditionStatus = metav1.ConditionTrue
		}
	}
	r.components = append(r.components, rc)
	return r
}

func (r *Reconciler) TemplateComponent(template string, conditionType string) *Reconciler {
	name := template[:strings.LastIndex(template, ".")]
	return r.Component(name, NewTemplateComponent(template, conditionType))
}

func (r *Reconciler) RandomSecretComponent(keys ...string) *Reconciler {
	// TODO This is super awkward. Maybe just provisionally set r.name from For()?
	controllerName, err := r.getControllerName()
	if err != nil {
		panic(err)
	}
	nameTemplate := fmt.Sprintf("%%s-%s", controllerName)
	return r.Component("randomSecret", NewRandomSecretComponent(nameTemplate, keys...))
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

	// Work out a default finalizer base name.
	if r.finalizerBaseName == "" {
		gvk, err := apiutil.GVKForObject(r.apiType, r.mgr.GetScheme())
		if err != nil {
			return nil, errors.Wrapf(err, "error getting GVK for object %#v", r.apiType)
		}
		r.finalizerBaseName = fmt.Sprintf("%s.%s/", name, gvk.Group)
	}

	// Check if we have more than component with the same name.
	compMap := map[string]Component{}
	for _, rc := range r.components {
		first, ok := compMap[rc.name]
		if ok {
			return nil, errors.Errorf("found duplicate component using name %s: %#v %#v", rc.name, first, rc.comp)
		}
		compMap[rc.name] = rc.comp
	}

	setupCtx := &Context{
		Context:        context.Background(),
		Client:         r.client,
		UncachedClient: r.uncachedClient,
		Templates:      r.templates,
		Scheme:         r.mgr.GetScheme(),
		Object:         r.apiType.DeepCopyObject().(Object),
	}
	// Provide some bare minimum data
	setupObj := setupCtx.Object.(metav1.Object)
	setupObj.SetName("setup")
	setupObj.SetNamespace("setup")
	log := r.log.WithName("components")
	for _, rc := range r.components {
		rc.finalizerName = r.finalizerBaseName + rc.name
		setupComp, ok := rc.comp.(InitializerComponent)
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
	// If requested, set up a webhook runable too.
	if r.webhook {
		err := ctrl.NewWebhookManagedBy(r.mgr).For(r.apiType).Complete()
		if err != nil {
			return nil, errors.Wrap(err, "error initializing webhook")
		}
	}
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
	ctx.Object = obj.(Object)
	ctx.Conditions = NewConditionsHelper(ctx.Object)
	cleanObj := obj.DeepCopyObject().(Object)

	// Check for annotation that blocks reconciles, exit early if found.
	annotations := ctx.Object.GetAnnotations()
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
		isAlive := ctx.Object.GetDeletionTimestamp() == nil
		if rc.readyCondition != "" {
			ctx.Conditions.SetUnknown(rc.readyCondition, "Unknown")
		}
		var res Result
		if isAlive {
			log.V(1).Info("Reconciling component", "component", rc.name)
			res, err = rc.comp.Reconcile(ctx)
			if rc.finalizer != nil {
				controllerutil.AddFinalizer(ctx.Object, rc.finalizerName)
			}
		} else if rc.finalizer != nil && controllerutil.ContainsFinalizer(ctx.Object, rc.finalizerName) {
			log.V(1).Info("Finalizing component", "component", rc.name)
			var done bool
			res, done, err = rc.finalizer.Finalize(ctx)
			if done {
				controllerutil.RemoveFinalizer(ctx.Object, rc.finalizerName)
			}
		}
		if err != nil && rc.readyCondition != "" {
			// Mark the status condition for this component as bad.
			ctx.Conditions.Set(rc.readyCondition, rc.errorConditionStatus, "Error", err.Error())
		}
		ctx.mergeResult(rc.name, res, err)
		if err != nil {
			log.Error(err, "error in component reconcile", "component", rc.name)
		}
		if res.SkipRemaining {
			// Abort reconcile to skip remaining components.
			log.V(1).Info("Skipping remaining components")
			break
		}
	}

	// Check if we need to patch metadata, only looking at labels, annotations, and finalizers.
	currentMeta := r.apiType.DeepCopyObject().(Object)
	currentMeta.SetName(ctx.Object.GetName())
	currentMeta.SetNamespace(ctx.Object.GetNamespace())
	currentMeta.SetLabels(ctx.Object.GetLabels())
	currentMeta.SetAnnotations(ctx.Object.GetAnnotations())
	currentMeta.SetFinalizers(ctx.Object.GetFinalizers())
	cleanMeta := r.apiType.DeepCopyObject().(Object)
	cleanMeta.SetName(cleanObj.GetName())
	cleanMeta.SetNamespace(cleanObj.GetNamespace())
	cleanMeta.SetLabels(cleanObj.GetLabels())
	cleanMeta.SetAnnotations(cleanObj.GetAnnotations())
	cleanMeta.SetFinalizers(cleanObj.GetFinalizers())
	err = r.client.Patch(ctx, currentMeta, client.MergeFrom(cleanMeta), &client.PatchOptions{FieldManager: r.name})
	if err != nil && !kerrors.IsNotFound(err) {
		// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
		return ctx.result, errors.Wrap(err, "error patching metadata")
	}

	// Save the object status.
	err = r.client.Status().Patch(ctx, ctx.Object, client.MergeFrom(cleanObj), &client.PatchOptions{FieldManager: r.name})
	if err != nil && !kerrors.IsNotFound(err) {
		// If it was a NotFound error, the object was probably already deleted so just ignore the error and return the existing result.
		return ctx.result, errors.Wrap(err, "error patching status")
	}

	// Build up the final error to be logged.
	err = nil
	if len(ctx.errors) == 1 {
		err = ctx.errors[0]
	} else if len(ctx.errors) > 1 {
		msg := strings.Builder{}
		msg.WriteString("Multiple errors:\n")
		for _, e := range ctx.errors {
			msg.WriteString("  ")
			msg.WriteString(e.Error())
			msg.WriteString("\n")
		}
		err = errors.New(msg.String())
	}

	return ctx.result, err
}
