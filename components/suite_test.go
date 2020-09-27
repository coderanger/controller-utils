/*
Copyright 2020 Geomagical Labs Inc.

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
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/coderanger/controller-utils/core"
	"github.com/coderanger/controller-utils/tests"
)

// Despite being unit-ish tests, these have to use the functional subsystem because there
// is no fake implementation of server-side apply.
var suiteHelper *tests.FunctionalSuiteHelper

func TestComponents(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Components Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	ctrl.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	suiteHelper = tests.Functional().MustBuild()

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	suiteHelper.MustStop()
})

func newTestController(components ...core.Component) func(ctrl.Manager) error {
	return func(mgr ctrl.Manager) error {
		b := core.NewReconciler(mgr).For(&corev1.ConfigMap{})
		for i, comp := range components {
			b = b.Component(fmt.Sprintf("test%d", i), comp)
		}
		return b.Complete()
	}
}

func startTestController(components ...core.Component) *tests.FunctionalHelper {
	return suiteHelper.MustStart(newTestController(components...))
}
