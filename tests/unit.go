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

package tests

import (
	"context"
	"net/http"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/coderanger/controller-utils/core"
)

type unitBuilder struct {
	apis      []schemeAdder
	templates http.FileSystem
}

type UnitSuiteHelper struct {
	scheme    *runtime.Scheme
	templates http.FileSystem
}

type UnitHelper struct {
	Comp       core.Component
	Client     client.Client
	TestClient *testClient
	Object     core.Object
	Events     chan string
	Ctx        *core.Context
}

func Unit() *unitBuilder {
	return &unitBuilder{}
}

func (b *unitBuilder) API(adder schemeAdder) *unitBuilder {
	b.apis = append(b.apis, adder)
	return b
}

func (b *unitBuilder) Templates(templates http.FileSystem) *unitBuilder {
	b.templates = templates
	return b
}

func (b *unitBuilder) Build() (*UnitSuiteHelper, error) {
	sch := runtime.NewScheme()

	// Register the default scheme things.
	err := scheme.AddToScheme(sch)
	if err != nil {
		return nil, errors.Wrap(err, "error adding default scheme")
	}

	// Add all requested APIs to the global scheme.
	for _, adder := range b.apis {
		err = adder(sch)
		if err != nil {
			return nil, errors.Wrap(err, "error adding scheme")
		}
	}

	return &UnitSuiteHelper{templates: b.templates, scheme: sch}, nil
}

func (b *unitBuilder) MustBuild() *UnitSuiteHelper {
	ush, err := b.Build()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return ush
}

func (ush *UnitSuiteHelper) Setup(comp core.Component, obj core.Object) *UnitHelper {
	uh := &UnitHelper{Comp: comp}

	metaObj := obj.(metav1.Object)
	if metaObj.GetName() == "" {
		metaObj.SetName("testing")
	}
	if metaObj.GetNamespace() == "" {
		metaObj.SetNamespace("default")
	}
	uh.Object = obj

	uh.Client = fake.NewFakeClientWithScheme(ush.scheme, uh.Object)
	uh.TestClient = &testClient{client: uh.Client, namespace: metaObj.GetNamespace()}

	events := record.NewFakeRecorder(100)
	uh.Events = events.Events

	ctx := &core.Context{
		Context:      context.Background(),
		Object:       uh.Object,
		Client:       uh.Client,
		Templates:    ush.templates,
		FieldManager: "unit-tests",
		Scheme:       ush.scheme,
		Data:         core.ContextData{},
		Events:       events,
		Conditions:   core.NewConditionsHelper(uh.Object),
	}
	uh.Ctx = ctx

	return uh
}

func (uh *UnitHelper) Reconcile() (core.Result, error) {
	defaulter, ok := uh.Object.(admission.Defaulter)
	if ok {
		defaulter.Default()
	}
	uh.TestClient.Update(uh.Object)
	res, err := uh.Comp.Reconcile(uh.Ctx)
	compErr := uh.Ctx.Conditions.Flush()
	if compErr != nil && err == nil {
		err = compErr
	}
	return res, err
}

func (uh *UnitHelper) MustReconcile() core.Result {
	res, err := uh.Reconcile()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return res
}
