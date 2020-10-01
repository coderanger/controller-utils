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
	"math/rand"
	"path/filepath"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
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
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/coderanger/controller-utils/randstring"
)

type schemeAdder func(*runtime.Scheme) error
type managerAdder func(ctrl.Manager) error

type functionalBuilder struct {
	crdPaths     []string
	crds         []runtime.Object
	webhookPaths []string
	apis         []schemeAdder
}

type FunctionalSuiteHelper struct {
	environment *envtest.Environment
	cfg         *rest.Config
}

type FunctionalHelper struct {
	managerStop    chan struct{}
	managerDone    chan struct{}
	UncachedClient client.Client
	Client         client.Client
	TestClient     *testClient
	Namespace      string
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

func (b *functionalBuilder) WebhookPaths(path string) *functionalBuilder {
	b.webhookPaths = append(b.webhookPaths, path)
	return b
}

func (b *functionalBuilder) API(adder schemeAdder) *functionalBuilder {
	b.apis = append(b.apis, adder)
	return b
}

func (b *functionalBuilder) Build() (*FunctionalSuiteHelper, error) {
	helper := &FunctionalSuiteHelper{}
	// Set up default paths for standard kubebuilder usage.
	if len(b.crdPaths) == 0 {
		b.crdPaths = append(b.crdPaths, filepath.Join("..", "config", "crd", "bases"))
	}
	var defaultWebhookPaths bool
	if len(b.webhookPaths) == 0 {
		b.webhookPaths = append(b.webhookPaths, filepath.Join("..", "config", "webhook"))
		defaultWebhookPaths = true
	}

	// Configure the test environment.
	helper.environment = &envtest.Environment{
		CRDDirectoryPaths: b.crdPaths,
		CRDs:              b.crds,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			DirectoryPaths:           b.webhookPaths,
			IgnoreErrorIfPathMissing: defaultWebhookPaths,
		},
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

	// Pick a randomize namespace so tests don't cross-talk as much.
	fh.Namespace = "test-" + randstring.MustRandomString(10)

	mgr, err := manager.New(fsh.cfg, manager.Options{
		// Disable both listeners so tests don't raise a "Do you want to allow ... to listen" dialog on macOS.
		MetricsBindAddress:     "0",
		HealthProbeBindAddress: "0",
		Namespace:              fh.Namespace,
		Host:                   fsh.environment.WebhookInstallOptions.LocalServingHost,
		Port:                   fsh.environment.WebhookInstallOptions.LocalServingPort,
		CertDir:                fsh.environment.WebhookInstallOptions.LocalServingCertDir,
		LeaderElection:         false,
	})
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
	fh.UncachedClient, err = client.New(fsh.cfg, client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		return nil, errors.Wrap(err, "error creating raw client")
	}

	// Create the actual random namespace.
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: fh.Namespace}}
	err = fh.UncachedClient.Create(context.Background(), namespace)
	if err != nil {
		return nil, errors.Wrapf(err, "error creating test namespace %s", fh.Namespace)
	}

	// Create a namespace-bound test client.
	fh.TestClient = &testClient{client: fh.Client, namespace: fh.Namespace}

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
	// TODO This is not needed in controller-runtime 0.6 or above, revisit.
	metrics.Registry = prometheus.NewRegistry()
	return nil
}

func (fh *FunctionalHelper) MustStop() {
	err := fh.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}
