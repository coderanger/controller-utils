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
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/yaml"

	"github.com/coderanger/controller-utils/randstring"
)

type schemeAdder func(*runtime.Scheme) error
type managerAdder func(ctrl.Manager) error

type functionalBuilder struct {
	crdPaths     []string
	crds         []*apiextv1.CustomResourceDefinition
	webhookPaths []string
	apis         []schemeAdder
	externalName *string
}

type FunctionalSuiteHelper struct {
	environment *envtest.Environment
	cfg         *rest.Config
	external    bool
}

type FunctionalHelper struct {
	managerCancel  context.CancelFunc
	managerDone    chan struct{}
	UncachedClient client.Client
	Client         client.Client
	TestClient     *testClient
	Namespace      string
	namespaceObj   *corev1.Namespace
}

func Functional() *functionalBuilder {
	return &functionalBuilder{}
}

func (b *functionalBuilder) CRDPath(path string) *functionalBuilder {
	b.crdPaths = append(b.crdPaths, path)
	return b
}

func (b *functionalBuilder) CRD(crd *apiextv1.CustomResourceDefinition) *functionalBuilder {
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

func (b *functionalBuilder) UseExistingCluster(externalName string) *functionalBuilder {
	b.externalName = &externalName
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
			Paths:                    b.webhookPaths,
			IgnoreErrorIfPathMissing: defaultWebhookPaths,
		},
	}
	if b.externalName != nil {
		boolp := true
		helper.environment.UseExistingCluster = &boolp
		helper.environment.WebhookInstallOptions.LocalServingHost = "0.0.0.0"
		helper.environment.WebhookInstallOptions.LocalServingHostExternalName = *b.externalName
		helper.external = true
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
	ctx, cancel := context.WithCancel(context.Background())
	fh.managerCancel = cancel
	fh.managerDone = make(chan struct{})
	go func() {
		defer close(fh.managerDone)
		err := mgr.Start(ctx)
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
	if fsh.external {
		fh.namespaceObj = namespace
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
	// Clean up the namespace if using an extneral control plane.
	if fh.namespaceObj != nil {
		err := fh.UncachedClient.Delete(context.Background(), fh.namespaceObj)
		if err != nil {
			return err
		}
	}
	if fh != nil && fh.managerCancel != nil {
		fh.managerCancel()
		// TODO maybe replace this with my own timeout so it doesn't use Gomega.
		gomega.Eventually(fh.managerDone, 30*time.Second).Should(gomega.BeClosed())
	}
	// TODO This is not needed in controller-runtime 0.6 or above, revisit.
	metrics.Registry = prometheus.NewRegistry()
	return nil
}

func (fh *FunctionalHelper) MustStop() {
	err := fh.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

// Helper method to show a list of objects, used in AfterEach helpers.
func (fh *FunctionalHelper) DebugList(listType runtime.Object) {
	gvks, unversioned, err := scheme.Scheme.ObjectKinds(listType)
	if err != nil {
		fmt.Printf("DebugList Error: %v", err)
		panic(err)
	}
	if unversioned || len(gvks) == 0 {
		fmt.Println("DebugList Error: Error getting GVKs")
		panic("Error getting GVKs")
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvks[0])

	err = fh.UncachedClient.List(context.Background(), list)
	if err != nil {
		fmt.Printf("DebugList Error: %v", err)
		panic(err)
	}

	output := map[string]interface{}{}
	for _, item := range list.Items {
		meta := item.Object["metadata"].(map[string]interface{})
		if meta["namespace"].(string) == fh.Namespace {
			output[meta["name"].(string)] = item.Object
		}
	}
	outputBytes, err := yaml.Marshal(output)
	if err != nil {
		fmt.Printf("DebugList Error: %v", err)
		panic(err)
	}
	fmt.Printf("\n%s\n%s\n%s\n", gvks[0].Kind, strings.Repeat("=", len(gvks[0].Kind)), string(outputBytes))
}
