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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/coderanger/controller-utils/core"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const RANDOM_BYTES = 32

// Lossy base64 endcoding to make passwords that will work basically anywhere.
var RandEncoding = base64.NewEncoding("abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijkl").WithPadding(base64.NoPadding)

type randomSecretComponent struct {
	name string
	keys []string
}

func NewRandomSecretComponent(name string, keys ...string) core.Component {
	if len(keys) == 0 {
		// Default key if none are specified.
		keys = []string{"password"}
	}
	return &randomSecretComponent{name, keys}
}

func (comp *randomSecretComponent) Setup(_ *core.Context, bldr *ctrl.Builder) error {
	bldr.Owns(&corev1.Secret{})
	return nil
}

func (comp *randomSecretComponent) Reconcile(ctx *core.Context) (core.Result, error) {
	obj := ctx.Object.(metav1.Object)

	name := comp.name
	if strings.Contains(name, "%s") {
		name = fmt.Sprintf(name, obj.GetName())
	}

	secretName := types.NamespacedName{
		Name:      name,
		Namespace: obj.GetNamespace(),
	}
	existingSecret := &corev1.Secret{}
	err := ctx.Client.Get(ctx, secretName, existingSecret)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Patch will create it so no need for anything else specific.
		} else {
			return core.Result{}, errors.Wrapf(err, "error getting secret %s", secretName)
		}
	}

	data := map[string][]byte{}

	for _, key := range comp.keys {
		val, ok := existingSecret.Data[key]
		if !ok || len(val) == 0 {
			raw := make([]byte, RANDOM_BYTES)
			_, err := rand.Read(raw)
			if err != nil {
				return core.Result{}, errors.Wrap(err, "error generating random bytes")
			}
			val = make([]byte, RandEncoding.EncodedLen(RANDOM_BYTES))
			RandEncoding.Encode(val, raw)
		}
		data[key] = val
		// Store the values into context for use by later components.
		ctx.Data[key] = string(val)
	}

	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"type": "Opaque",
			"data": data,
		},
	}
	secret.SetName(secretName.Name)
	secret.SetNamespace(secretName.Namespace)
	secret.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"})

	err = controllerutil.SetControllerReference(obj, secret, ctx.Scheme)
	if err != nil {
		return core.Result{}, errors.Wrap(err, "error setting owner reference")
	}

	// Sigh *bool.
	force := true
	err = ctx.Client.Patch(ctx, secret, client.Apply, &client.PatchOptions{Force: &force, FieldManager: ctx.FieldManager})
	if err != nil {
		return core.Result{}, errors.Wrapf(err, "error applying secret %s", secretName)
	}

	return core.Result{}, nil
}

func init() {
	// Avoid import loops.
	core.NewRandomSecretComponent = NewRandomSecretComponent
}
