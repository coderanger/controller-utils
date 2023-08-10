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

package matchers

import (
	"fmt"

	"github.com/onsi/gomega"

	"github.com/coderanger/controller-utils/conditions"
	"github.com/coderanger/controller-utils/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type haveConditionMatcher struct {
	conditionType string
	status        *string
	reason        *string
}

func HaveCondition(conditionType string) *haveConditionMatcher {
	return &haveConditionMatcher{conditionType: conditionType}
}

func (matcher *haveConditionMatcher) WithStatus(status string) *haveConditionMatcher {
	matcher.status = &status
	return matcher
}

func (matcher *haveConditionMatcher) WithReason(reason string) *haveConditionMatcher {
	matcher.reason = &reason
	return matcher
}

func (matcher *haveConditionMatcher) Match(actual interface{}) (bool, error) {
	obj, ok := actual.(client.Object)
	if !ok {
		return false, fmt.Errorf("HaveCondition matcher expects a client.Object")
	}
	conds, err := core.GetConditionsFor(obj)
	if err != nil {
		return false, err
	}

	cond := conditions.FindStatusCondition(*conds, matcher.conditionType)
	if cond == nil {
		return false, nil
	}

	if matcher.status != nil {
		match, err := gomega.BeEquivalentTo(*matcher.status).Match(cond.Status)
		if !match || err != nil {
			return match, err
		}
	}

	if matcher.reason != nil {
		match, err := gomega.Equal(*matcher.reason).Match(cond.Reason)
		if !match || err != nil {
			return match, err
		}
	}

	return true, nil
}

func (matcher *haveConditionMatcher) FailureMessage(actual interface{}) string {
	return matcher.message(actual, true)
}

func (matcher *haveConditionMatcher) NegatedFailureMessage(actual interface{}) string {
	return matcher.message(actual, false)
}

func (matcher *haveConditionMatcher) message(actual interface{}, polarity bool) string {
	filters := ""
	if matcher.status != nil {
		filters += fmt.Sprintf(" with status %s", *matcher.status)
	}
	if matcher.reason != nil {
		filters += fmt.Sprintf(" with reason %s", *matcher.reason)
	}

	joiner := ""
	if !polarity {
		joiner = "not "
	}

	obj, ok := actual.(client.Object)
	if ok {
		conds, err := core.GetConditionsFor(obj)
		if err == nil {
			actual = *conds
		}
	}

	return fmt.Sprintf("Expected %#v to %shave condition %s%s", actual, joiner, matcher.conditionType, filters)
}
