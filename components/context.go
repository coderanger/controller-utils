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
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ContextData map[string]interface{}

type Context struct {
	context.Context
	Object runtime.Object // Change this to controllerutil.Object after bumping to 0.6
	Client client.Client
	Log    logr.Logger
	// Pending result at the end of things.
	result ctrl.Result
	// Most recent component error, if any.
	err error
	// Templates filesystem, mostly used through helpers but accessible directly too.
	Templates http.FileSystem
	// Name to use as the field manager with Apply.
	FieldManager string
	// API Scheme for use with other helpers.
	Scheme *runtime.Scheme
	// Arbitrary data used to communicate between components during a reconcile.
	Data ContextData
}

func (c *Context) mergeResult(componentResult Result, err error) {
	if err != nil {
		c.err = err
	}
	if componentResult.Requeue {
		c.result.Requeue = true
	}
	if componentResult.RequeueAfter != 0 && (c.result.RequeueAfter == 0 || c.result.RequeueAfter > componentResult.RequeueAfter) {
		c.result.RequeueAfter = componentResult.RequeueAfter
	}
}

func (d ContextData) GetString(key string) (string, bool) {
	val, ok := d[key]
	return val.(string), ok
}
