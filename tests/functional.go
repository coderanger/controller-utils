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
	"encoding/hex"
	"math/rand"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type schemeAdder func(*runtime.Scheme) error
type managerAdder func(ctrl.Manager) error

type functionalBuilder struct {
	crdPaths []string
	crds     []runtime.Object
	apis     []schemeAdder
}

type FunctionalSuiteHelper struct {
	environment *envtest.Environment
	cfg         *rest.Config
}

type FunctionalHelper struct {
	managerStop chan struct{}
	managerDone chan struct{}
	RawClient   client.Client
	Client      client.Client
	TestClient  *testClient
	Namespace   string
}

func Functional() *functionalBuilder {
	return &functionalBuilder{}
}

func (b *functionalBuilder) CRDPath(path string) *functionalBuilder {
	b.crdPaths = append(b.crdPaths, path)
	return b
}

func (b *functionalBuilder) CRD(crd *apiextv1beta1.CustomResourceDefinition) *functionalBuilder {
	b.crds = append(b.crds, crd)
	return b
}

func (b *functionalBuilder) API(adder schemeAdder) *functionalBuilder {
	b.apis = append(b.apis, adder)
	return b
}

func (b *functionalBuilder) Build() (*FunctionalSuiteHelper, error) {
	helper := &FunctionalSuiteHelper{}
	// Configure the test environment.
	helper.environment = &envtest.Environment{
		CRDDirectoryPaths: b.crdPaths,
		CRDs:              b.crds,
	}

	// Initialze the RNG.
	rand.Seed(time.Now().UnixNano())

	// Start the environment.
	var err error
	helper.cfg, err = helper.environment.Start()
	if err != nil {
		return nil, errors.Wrap(err, "error starting environment")
	}

	// Add all requested APIs to the global scheme.
	for _, adder := range b.apis {
		err = adder(scheme.Scheme)
		if err != nil {
			return nil, errors.Wrap(err, "error adding scheme")
		}
	}

	return helper, nil
}

func (b *functionalBuilder) MustBuild() *FunctionalSuiteHelper {
	fsh, err := b.Build()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return fsh
}

func (fsh *FunctionalSuiteHelper) Stop() error {
	if fsh != nil && fsh.environment != nil {
		err := fsh.environment.Stop()
		if err != nil {
			return err
		}
	}
	return nil
}

func (fsh *FunctionalSuiteHelper) MustStop() {
	err := fsh.Stop()
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func (fsh *FunctionalSuiteHelper) Start(controllers ...managerAdder) (*FunctionalHelper, error) {
	fh := &FunctionalHelper{}

	mgr, err := manager.New(fsh.cfg, manager.Options{})
	if err != nil {
		return nil, errors.Wrap(err, "error creating manager")
	}

	// Add the requested controllers.
	for _, adder := range controllers {
		err := adder(mgr)
		if err != nil {
			return nil, errors.Wrap(err, "error adding controller")
		}
	}

	// Start the manager (in the background).
	fh.managerStop = make(chan struct{})
	fh.managerDone = make(chan struct{})
	go func() {
		defer close(fh.managerDone)
		err := mgr.Start(fh.managerStop)
		if err != nil {
			panic(err)
		}
	}()

	// Grab the clients.
	fh.Client = mgr.GetClient()
	fh.RawClient, err = client.New(fsh.cfg, client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		return nil, errors.Wrap(err, "error creating raw client")
	}

	// Create a random namespace to work in.
	namespaceNameBytes := make([]byte, 10)
	rand.Read(namespaceNameBytes)
	namespaceName := "test-" + hex.EncodeToString(namespaceNameBytes)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespaceName}}
	err = fh.RawClient.Create(context.Background(), namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "error creating test namespace %s", namespaceName)
	}

	// Create a namespace-bound test client.
	fh.TestClient = &testClient{client: fh.Client, namespace: namespaceName}

	return fh, nil
}

func (fsh *FunctionalSuiteHelper) MustStart(controllers ...managerAdder) *FunctionalHelper {
	fh, err := fsh.Start(controllers...)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	return fh
}

func (fh *FunctionalHelper) Stop() error {
	if fh != nil && fh.managerStop != nil {
		close(fh.managerStop)
		// TODO maybe replace this with my own timeout so it doesn't use Gomega.
		gomega.Eventually(fh.managerDone).Should(gomega.BeClosed())
	}
	return nil
}

func (fh *FunctionalHelper) MustStop() {
	err := fh.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}
