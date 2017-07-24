/*
Copyright 2017 The Kubernetes Authors.

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

package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	osb "github.com/pmorie/go-open-service-broker-client/v2"
	fakeosb "github.com/pmorie/go-open-service-broker-client/v2/fake"

	checksum "github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/checksum/versioned/v1alpha1"
	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"

	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	clientgotesting "k8s.io/client-go/testing"
)

const (
	lastOperationDescription = "testdescr"
)

func TestReconcileInstanceNonExistentServiceClass(t *testing.T) {
	_, fakeCatalogClient, fakeBrokerClient, testController, _ := newTestController(t, noFakeActions())

	instance := &v1alpha1.ServiceCatalogInstance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		Spec: v1alpha1.ServiceCatalogInstanceSpec{
			ServiceClassName: "nothere",
			PlanName:         "nothere",
			ExternalID:       instanceGUID,
		},
	}

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatal("nothere is a service class that cannot be referenced by the service instance as it does not exist.")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// There should only be one action that says it failed because no such class exists.
	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance, errorNonexistentServiceClassReason)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorNonexistentServiceClassReason + " " + "Instance \"/test-instance\" references a non-existent ServiceClass \"nothere\""
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceNonExistentBroker(t *testing.T) {
	_, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatal("The broker referenced by the instance exists when it should not.")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// There should only be one action that says it failed because no such broker exists.
	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance, errorNonexistentBrokerReason)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorNonexistentBrokerReason + " " + "Instance \"test-ns/test-instance\" references a non-existent broker \"test-broker\""
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceWithAuthError(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	broker := getTestBroker()
	broker.Spec.AuthInfo = &v1alpha1.BrokerAuthInfo{
		BasicAuthSecret: &v1.ObjectReference{
			Namespace: "does_not_exist",
			Name:      "auth-name",
		},
	}
	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(broker)
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	fakeKubeClient.AddReactor("get", "secrets", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("no secret defined")
	})

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatal("There was no secret to be found, but does_not_exist/auth-name was found.")
	}

	// verify that no broker actions occurred
	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	// verify that one catalog client action occurred
	actions := fakeCatalogClient.Actions()
	if err := checkCatalogClientActions(actions, []catalogClientAction{
		{
			verb: "update",
			checkObject: checkInstance(instanceDescription{
				name:             testInstanceName,
				conditionReasons: []string{"ErrorGettingAuthCredentials"},
			}),
			getRuntimeObject: getRuntimeObjectFromUpdateAction,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// verify one kube action occurred
	kubeActions := fakeKubeClient.Actions()
	if err := checkKubeClientActions(kubeActions, []kubeClientAction{
		{verb: "get", resourceName: "secrets", checkType: checkGetActionType},
	}); err != nil {
		t.Fatal(err)
	}

	// verify that one event was emitted
	events := getRecordedEvents(testController)
	expectedEvent := api.EventTypeWarning + " " + errorAuthCredentialsReason + " " + "Error getting broker auth credentials for broker \"test-broker\": no secret defined"
	if err := checkEvents(events, []string{expectedEvent}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileInstanceNonExistentServicePlan(t *testing.T) {
	_, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := &v1alpha1.ServiceCatalogInstance{
		ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		Spec: v1alpha1.ServiceCatalogInstanceSpec{
			ServiceClassName: testServiceClassName,
			PlanName:         "nothere",
			ExternalID:       instanceGUID,
		},
	}

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatal("The service plan nothere should not exist to be referenced.")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	// ensure that the only action made on the catalog client was to set the condition on the
	// instance to indicate that the service plan doesn't exist
	actions := fakeCatalogClient.Actions()
	if err := checkCatalogClientActions(actions, []catalogClientAction{
		{
			verb:             "update",
			getRuntimeObject: getRuntimeObjectFromUpdateAction,
			checkObject: checkInstance(instanceDescription{
				name:             testInstanceName,
				conditionReasons: []string{errorNonexistentServicePlanReason},
			}),
		},
	}); err != nil {
		t.Fatal(err)
	}

	// check to make sure the only event sent indicated that the instance references a non-existent
	// service plan
	events := getRecordedEvents(testController)
	expectedEvent := api.EventTypeWarning + " " + errorNonexistentServicePlanReason + " " + "Instance \"/test-instance\" references a non-existent ServicePlan \"nothere\" on ServiceClass \"test-serviceclass\""
	if err := checkEvents(events, []string{expectedEvent}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileInstanceWithParameters(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Response: &osb.ProvisionResponse{},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	parameters := instanceParameters{Name: "test-param", Args: make(map[string]string)}
	parameters.Args["first"] = "first-arg"
	parameters.Args["second"] = "second-arg"

	b, err := json.Marshal(parameters)
	if err != nil {
		t.Fatalf("Failed to marshal parameters %v : %v", parameters, err)
	}
	instance.Spec.Parameters = &runtime.RawExtension{Raw: b}

	if err = testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": "test-ns",
		},
		Parameters: map[string]interface{}{
			"args": map[string]interface{}{
				"first":  "first-arg",
				"second": "second-arg",
			},
			"name": "test-param",
		},
	})

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyTrue(t, updatedInstance)

	updateObject, ok := updatedInstance.(*v1alpha1.ServiceCatalogInstance)
	if !ok {
		t.Fatalf("couldn't convert to *v1alpha1.ServiceCatalogInstance")
	}

	// Verify parameters are what we'd expect them to be, basically name, map with two values in it.
	if len(updateObject.Spec.Parameters.Raw) == 0 {
		t.Fatalf("Parameters was unexpectedly empty")
	}

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeNormal + " " + successProvisionReason + " " + "The instance was provisioned successfully"
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceWithInvalidParameters(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()
	parameters := instanceParameters{Name: "test-param", Args: make(map[string]string)}
	parameters.Args["first"] = "first-arg"
	parameters.Args["second"] = "second-arg"

	b, err := json.Marshal(parameters)
	if err != nil {
		t.Fatalf("Failed to marshal parameters %v : %v", parameters, err)
	}
	// corrupt the byte slice to begin with a '!' instead of an opening JSON bracket '{'
	b[0] = 0x21
	instance.Spec.Parameters = &runtime.RawExtension{Raw: b}

	if err = testController.reconcileInstance(instance); err == nil {
		t.Fatalf("this should fail due to a parse error")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorWithParameters + " " + "Failed to unmarshal Instance parameters"
	if e, a := expectedEvent, events[0]; !strings.Contains(a, e) { // event contains RawExtension, so just compare error message
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceWithProvisionCallFailure(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Error: errors.New("fake creation failure"),
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatalf("Should not be able to make the Instance.")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": "test-ns",
		},
	})

	// verify no kube resources created
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 1)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorErrorCallingProvisionReason + " " + "Error provisioning Instance \"test-ns/test-instance\" of ServiceClass \"test-serviceclass\" at Broker \"test-broker\": fake creation failure"
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceWithProvisionFailure(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Error: osb.HTTPStatusCodeError{
				StatusCode:   http.StatusConflict,
				ErrorMessage: strPtr("OutOfQuota"),
				Description:  strPtr("You're out of quota!"),
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": "test-ns",
		},
	})

	// verify one kube action occurred
	kubeActions := fakeKubeClient.Actions()
	if err := checkKubeClientActions(kubeActions, []kubeClientAction{
		{verb: "get", resourceName: "namespaces", checkType: checkGetActionType},
	}); err != nil {
		t.Fatal(err)
	}

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)
	updatedObject := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedObject)
	updatedInstance, ok := updatedObject.(*v1alpha1.ServiceCatalogInstance)
	if !ok {
		t.Fatalf("couldn't convert to *v1alpha1.Instance")
	}
	if l := len(updatedInstance.Status.Conditions); l != 2 {
		t.Fatalf("Expected 2 conditions, got %v", l)
	}

	if updatedInstance.Status.Conditions[0].Type != v1alpha1.InstanceConditionFailed && updatedInstance.Status.Conditions[0].Status != v1alpha1.ConditionTrue {
		t.Fatalf("Expected failed condition to be set")
	}

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorProvisionCallFailedReason + " " + "Error provisioning Instance \"test-ns/test-instance\" of ServiceClass \"test-serviceclass\" at Broker \"test-broker\": Status: 409; ErrorMessage: OutOfQuota; Description: You're out of quota!; ResponseError: <nil>"
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstance(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Response: &osb.ProvisionResponse{
				DashboardURL: &testDashboardURL,
			},
		},
	})

	addGetNamespaceReaction(fakeKubeClient)

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		OrganizationGUID:  testNsUID,
		SpaceGUID:         testNsUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": "test-ns",
		},
	})

	// Since synchronous operation, must not make it into the polling queue.
	if testController.pollingQueue.Len() != 0 {
		t.Fatalf("Expected the polling queue to be empty")
	}

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created.
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyTrue(t, updatedInstance)
	assertInstanceDashboardURL(t, updatedInstance, testDashboardURL)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeNormal + " " + successProvisionReason + " " + successProvisionMessage
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceAsynchronous(t *testing.T) {
	key := osb.OperationKey(testOperation)
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Response: &osb.ProvisionResponse{
				Async:        true,
				DashboardURL: &testDashboardURL,
				OperationKey: &key,
			},
		},
	})

	addGetNamespaceReaction(fakeKubeClient)

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if testController.pollingQueue.Len() != 0 {
		t.Fatalf("Expected the polling queue to be empty")
	}

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		OrganizationGUID:  testNsUID,
		SpaceGUID:         testNsUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": testNamespace,
		},
	})

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created.
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	if e, a := 1, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance)

	// The item should've been added to the pollingQueue for later processing
	if testController.pollingQueue.Len() != 1 {
		t.Fatalf("Expected the asynchronous instance to end up in the polling queue")
	}
	item, _ := testController.pollingQueue.Get()
	if item == nil {
		t.Fatalf("Did not get back a key from polling queue")
	}
	actualKey := item.(string)
	expectedKey := fmt.Sprintf("%s/%s", instance.Namespace, instance.Name)
	if actualKey != expectedKey {
		t.Fatalf("got key as %q expected %q", actualKey, expectedKey)
	}
	assertAsyncOpInProgressTrue(t, updatedInstance)
	assertInstanceDashboardURL(t, updatedInstance, testDashboardURL)
	assertInstanceLastOperation(t, updatedInstance, testOperation)
}

func TestReconcileInstanceAsynchronousNoOperation(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		ProvisionReaction: &fakeosb.ProvisionReaction{
			Response: &osb.ProvisionResponse{
				Async:        true,
				DashboardURL: &testDashboardURL,
			},
		},
	})

	addGetNamespaceReaction(fakeKubeClient)

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if testController.pollingQueue.Len() != 0 {
		t.Fatalf("Expected the polling queue to be empty")
	}

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertProvision(t, brokerActions[0], &osb.ProvisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
		OrganizationGUID:  testNsUID,
		SpaceGUID:         testNsUID,
		Context: map[string]interface{}{
			"platform":  "kubernetes",
			"namespace": "test-ns",
		},
	})

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created.
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	if e, a := 1, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance)

	// The item should've been added to the pollingQueue for later processing
	if testController.pollingQueue.Len() != 1 {
		t.Fatalf("Expected the asynchronous instance to end up in the polling queue")
	}
	item, _ := testController.pollingQueue.Get()
	if item == nil {
		t.Fatalf("Did not get back a key from polling queue")
	}
	key := item.(string)
	expectedKey := fmt.Sprintf("%s/%s", instance.Namespace, instance.Name)
	if key != expectedKey {
		t.Fatalf("got key as %q expected %q", key, expectedKey)
	}
	assertAsyncOpInProgressTrue(t, updatedInstance)
	assertInstanceLastOperation(t, updatedInstance, "")
}

func TestReconcileInstanceNamespaceError(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	fakeKubeClient.AddReactor("get", "namespaces", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, &v1.Namespace{}, errors.New("No namespace")
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()

	if err := testController.reconcileInstance(instance); err == nil {
		t.Fatalf("There should not be a namespace for the Instance to be created in.")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	// verify no kube resources created.
	// One single action comes from getting namespace uid
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 1)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	assertUpdateStatus(t, actions[0], instance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeWarning + " " + errorFindingNamespaceInstanceReason + " " + "Failed to get namespace \"test-ns\" during instance create: No namespace"
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

func TestReconcileInstanceDelete(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		DeprovisionReaction: &fakeosb.DeprovisionReaction{
			Response: &osb.DeprovisionResponse{},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()
	instance.ObjectMeta.DeletionTimestamp = &metav1.Time{}
	instance.ObjectMeta.Finalizers = []string{v1alpha1.FinalizerServiceCatalog}
	// we only invoke the broker client to deprovision if we have a checksum set
	// as that implies a previous success.
	checksum := checksum.InstanceSpecChecksum(instance.Spec)
	instance.Status.Checksum = &checksum

	fakeCatalogClient.AddReactor("get", "servicecataloginstances", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, instance, nil
	})

	err := testController.reconcileInstance(instance)
	if err != nil {
		t.Fatalf("This should not fail")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertDeprovision(t, brokerActions[0], &osb.DeprovisionRequest{
		AcceptsIncomplete: true,
		InstanceID:        instanceGUID,
		ServiceID:         serviceClassGUID,
		PlanID:            planGUID,
	})

	// Verify no core kube actions occurred
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	// The three actions should be:
	// 0. Updating the ready condition
	// 1. Get against the instance
	// 2. Removing the finalizer
	assertNumberOfActions(t, actions, 3)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyFalse(t, updatedInstance)

	assertGet(t, actions[1], instance)
	updatedInstance = assertUpdateStatus(t, actions[2], instance)
	assertEmptyFinalizers(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)

	expectedEvent := api.EventTypeNormal + " " + successDeprovisionReason + " " + "The instance was deprovisioned successfully"
	if e, a := expectedEvent, events[0]; e != a {
		t.Fatalf("Received unexpected event: %v", a)
	}
}

// TestReconcileInstanceDeleteDoesNotInvokeBroker verfies that if an instance is created that is never
// actually provisioned the instance is able to be deleted and is not blocked by any interaction with
// a broker (since its very likely that a broker never actually existed).
func TestReconcileInstanceDeleteDoesNotInvokeBroker(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstance()
	instance.ObjectMeta.DeletionTimestamp = &metav1.Time{}
	instance.ObjectMeta.Finalizers = []string{v1alpha1.FinalizerServiceCatalog}

	fakeCatalogClient.AddReactor("get", "servicecataloginstances", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, instance, nil
	})

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	// Verify no core kube actions occurred
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	// The three actions should be:
	// 0. Get against the instance
	// 1. Removing the finalizer
	assertNumberOfActions(t, actions, 2)

	assertGet(t, actions[0], instance)
	updatedInstance := assertUpdateStatus(t, actions[1], instance)
	assertEmptyFinalizers(t, updatedInstance)

	// no events because no external deprovision was needed
	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 0)
}

func TestReconcileInstanceWithFailureCondition(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, noFakeActions())

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceWithFailedStatus()

	if err := testController.reconcileInstance(instance); err != nil {
		t.Fatalf("This should not fail : %v", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 0)

	if testController.pollingQueue.Len() != 0 {
		t.Fatalf("Expected the polling queue to be empty")
	}

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 0)

	// verify no actions on the kube client
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 0)
}

func TestPollServiceInstanceInProgressProvisioningWithOperation(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State:       osb.StateInProgress,
				Description: strPtr(lastOperationDescription),
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncProvisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err == nil {
		t.Fatalf("Expected pollInstanceInternal to fail while in progress")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// Make sure we get an error which means it will get requeued.
	if !strings.Contains(err.Error(), "still in progress") {
		t.Fatalf("pollInstanceInternal failed but not with expected error, expected %q got %q", "still in progress", err)
	}

	// there should have been 1 action to update the status with the last operation description
	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)
}

func TestPollServiceInstanceSuccessProvisioningWithOperation(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State:       osb.StateSucceeded,
				Description: strPtr(lastOperationDescription),
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncProvisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	// Instance should be ready and there no longer is an async operation
	// in place.
	assertInstanceReadyTrue(t, updatedInstance)
	assertAsyncOpInProgressFalse(t, updatedInstance)
}

func TestPollServiceInstanceFailureProvisioningWithOperation(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State: osb.StateFailed,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncProvisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	// Instance should be not ready and there no longer is an async operation
	// in place.
	assertInstanceReadyFalse(t, updatedInstance)
	assertAsyncOpInProgressFalse(t, updatedInstance)
}

func TestPollServiceInstanceInProgressDeprovisioningWithOperationNoFinalizer(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State:       osb.StateInProgress,
				Description: strPtr(lastOperationDescription),
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err == nil {
		t.Fatalf("Expected pollInstanceInternal to fail while in progress")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// Make sure we get an error which means it will get requeued.
	if !strings.Contains(err.Error(), "still in progress") {
		t.Fatalf("pollInstanceInternal failed but not with expected error, expected %q got %q", "still in progress", err)
	}

	// there should have been 1 action to update the instance status with the last operation
	// description
	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)
	action := actions[0]
	updatedInstance := assertUpdateStatus(t, action, instance)
	assertInstanceReadyFalse(t, updatedInstance, asyncDeprovisioningMessage)

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)
}

func TestPollServiceInstanceSuccessDeprovisioningWithOperationNoFinalizer(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State: osb.StateSucceeded,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	// Instance should have been deprovisioned
	assertInstanceReadyCondition(t, updatedInstance, v1alpha1.ConditionFalse, successDeprovisionReason)
	assertAsyncOpInProgressFalse(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)
}

func TestPollServiceInstanceFailureDeprovisioningWithOperation(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State: osb.StateFailed,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	// Instance should be set to unknown since the operation on the broker
	// failed.
	assertInstanceReadyCondition(t, updatedInstance, v1alpha1.ConditionUnknown, errorDeprovisionCalledReason)
	assertAsyncOpInProgressFalse(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)
}

func TestPollServiceInstanceStatusGoneDeprovisioningWithOperationNoFinalizer(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Error: osb.HTTPStatusCodeError{
				StatusCode: http.StatusGone,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 1)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	// Instance should have been deprovisioned
	assertInstanceReadyCondition(t, updatedInstance, v1alpha1.ConditionFalse, successDeprovisionReason)
	assertAsyncOpInProgressFalse(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)
}

func TestPollServiceInstanceBrokerError(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Error: osb.HTTPStatusCodeError{
				StatusCode: http.StatusForbidden,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioning(testOperation)

	err := testController.pollInstanceInternal(instance)
	if err == nil {
		t.Fatal("expected pollInstanceInternal to return an error")
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)
	assertPollLastOperation(t, brokerActions[0], &osb.LastOperationRequest{
		InstanceID: instanceGUID,
		ServiceID:  strPtr(serviceClassGUID),
		PlanID:     strPtr(planGUID),
	})

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	assertNumberOfActions(t, actions, 0)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)
}

func TestPollServiceInstanceSuccessDeprovisioningWithOperationWithFinalizer(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerClient, testController, sharedInformers := newTestController(t, fakeosb.FakeClientConfiguration{
		PollLastOperationReaction: &fakeosb.PollLastOperationReaction{
			Response: &osb.LastOperationResponse{
				State: osb.StateSucceeded,
			},
		},
	})

	sharedInformers.ServiceCatalogBrokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceCatalogServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := getTestInstanceAsyncDeprovisioningWithFinalizer(testOperation)
	// updateInstanceFinalizers fetches the latest object.
	fakeCatalogClient.AddReactor("get", "servicecataloginstances", func(action clientgotesting.Action) (bool, runtime.Object, error) {
		return true, instance, nil
	})

	err := testController.pollInstanceInternal(instance)
	if err != nil {
		t.Fatalf("pollInstanceInternal failed: %s", err)
	}

	brokerActions := fakeBrokerClient.Actions()
	assertNumberOfBrokerActions(t, brokerActions, 1)

	// verify no kube resources created.
	// No actions
	kubeActions := fakeKubeClient.Actions()
	assertNumberOfActions(t, kubeActions, 0)

	actions := fakeCatalogClient.Actions()
	// The three actions should be:
	// 0. Updating the ready condition
	// 1. Get against the instance (updateFinalizers calls)
	// 2. Removing the finalizer
	assertNumberOfActions(t, actions, 3)

	updatedInstance := assertUpdateStatus(t, actions[0], instance)
	assertInstanceReadyCondition(t, updatedInstance, v1alpha1.ConditionFalse, successDeprovisionReason)

	// Instance should have been deprovisioned
	assertGet(t, actions[1], instance)
	updatedInstance = assertUpdateStatus(t, actions[2], instance)
	assertEmptyFinalizers(t, updatedInstance)

	events := getRecordedEvents(testController)
	assertNumEvents(t, events, 1)
}

func TestSetInstanceCondition(t *testing.T) {
	instanceWithCondition := func(condition *v1alpha1.InstanceCondition) *v1alpha1.ServiceCatalogInstance {
		instance := getTestInstance()
		instance.Status = v1alpha1.ServiceCatalogInstanceStatus{
			Conditions: []v1alpha1.InstanceCondition{*condition},
		}

		return instance
	}

	newTs := metav1.Now()

	condition := func(cType v1alpha1.InstanceConditionType, status v1alpha1.ConditionStatus, s ...string) *v1alpha1.InstanceCondition {
		c := &v1alpha1.InstanceCondition{
			Type:   cType,
			Status: status,
		}

		if len(s) > 0 {
			c.Reason = s[0]
		}

		if len(s) > 1 {
			c.Message = s[1]
		}

		c.LastTransitionTime = metav1.NewTime(newTs.Add(-5 * time.Minute))

		return c
	}

	readyFalse := func() *v1alpha1.InstanceCondition {
		return condition(v1alpha1.InstanceConditionReady, v1alpha1.ConditionFalse, "Reason", "Message")
	}

	readyFalsef := func(reason, message string) *v1alpha1.InstanceCondition {
		return condition(v1alpha1.InstanceConditionReady, v1alpha1.ConditionFalse, reason, message)
	}

	readyTrue := func() *v1alpha1.InstanceCondition {
		return condition(v1alpha1.InstanceConditionReady, v1alpha1.ConditionTrue, "Reason", "Message")
	}

	failedTrue := func() *v1alpha1.InstanceCondition {
		return condition(v1alpha1.InstanceConditionFailed, v1alpha1.ConditionTrue, "Reason", "Message")
	}

	// this test works by calling setInstanceCondition with the input and
	// condition fields of the test case, and ensuring that afterward the
	// input (which is mutated by the setInstanceCondition call) is deep-equal
	// to the test case result.
	cases := []struct {
		name      string
		input     *v1alpha1.ServiceCatalogInstance
		condition *v1alpha1.InstanceCondition
		result    *v1alpha1.ServiceCatalogInstance
	}{
		{
			name:      "new ready condition",
			input:     getTestInstance(),
			condition: readyFalse(),
			result: func() *v1alpha1.ServiceCatalogInstance {
				i := instanceWithCondition(readyFalse())
				i.Status.Conditions[0].LastTransitionTime = newTs
				return i
			}(),
		},
		{
			name:      "not ready -> not ready",
			input:     instanceWithCondition(readyFalse()),
			condition: readyTrue(),
			result: func() *v1alpha1.ServiceCatalogInstance {
				i := instanceWithCondition(readyTrue())
				i.Status.Conditions[0].LastTransitionTime = newTs
				return i
			}(),
		},
		{
			name:      "not ready -> not ready, reason and message change",
			input:     instanceWithCondition(readyFalse()),
			condition: readyFalsef("DifferentReason", "DifferentMessage"),
			result:    instanceWithCondition(readyFalsef("DifferentReason", "DifferentMessage")),
		},
		{
			name:      "not ready -> ready",
			input:     instanceWithCondition(readyFalse()),
			condition: readyTrue(),
			result: func() *v1alpha1.ServiceCatalogInstance {
				i := instanceWithCondition(readyTrue())
				i.Status.Conditions[0].LastTransitionTime = newTs
				return i
			}(),
		},
		{
			name:      "ready -> ready",
			input:     instanceWithCondition(readyTrue()),
			condition: readyTrue(),
			result:    instanceWithCondition(readyTrue()),
		},
		{
			name:      "ready -> not ready",
			input:     instanceWithCondition(readyTrue()),
			condition: readyFalse(),
			result: func() *v1alpha1.ServiceCatalogInstance {
				i := instanceWithCondition(readyFalse())
				i.Status.Conditions[0].LastTransitionTime = newTs
				return i
			}(),
		},
		{
			name:      "not ready + failed",
			input:     instanceWithCondition(readyFalse()),
			condition: failedTrue(),
			result: func() *v1alpha1.ServiceCatalogInstance {
				i := instanceWithCondition(readyFalse())
				i.Status.Conditions = append(i.Status.Conditions, *failedTrue())
				i.Status.Conditions[1].LastTransitionTime = newTs
				return i
			}(),
		},
	}

	for _, tc := range cases {
		setInstanceConditionInternal(tc.input, tc.condition.Type, tc.condition.Status, tc.condition.Reason, tc.condition.Message, newTs)

		if !reflect.DeepEqual(tc.input, tc.result) {
			t.Errorf("%v: unexpected diff: %v", tc.name, diff.ObjectReflectDiff(tc.input, tc.result))
		}
	}
}

func TestUpdateInstanceCondition(t *testing.T) {
	getTestInstanceWithStatus := func(status v1alpha1.ConditionStatus) *v1alpha1.ServiceCatalogInstance {
		instance := getTestInstance()
		instance.Status = v1alpha1.ServiceCatalogInstanceStatus{
			Conditions: []v1alpha1.InstanceCondition{{
				Type:               v1alpha1.InstanceConditionReady,
				Status:             status,
				Message:            "message",
				LastTransitionTime: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
			}},
		}

		return instance
	}

	cases := []struct {
		name                  string
		input                 *v1alpha1.ServiceCatalogInstance
		status                v1alpha1.ConditionStatus
		reason                string
		message               string
		transitionTimeChanged bool
	}{

		{
			name:                  "initially unset",
			input:                 getTestInstance(),
			status:                v1alpha1.ConditionFalse,
			message:               "message",
			transitionTimeChanged: true,
		},
		{
			name:                  "not ready -> not ready",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionFalse),
			status:                v1alpha1.ConditionFalse,
			transitionTimeChanged: false,
		},
		{
			name:                  "not ready -> not ready, reason and message change",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionFalse),
			status:                v1alpha1.ConditionFalse,
			reason:                "foo",
			message:               "bar",
			transitionTimeChanged: false,
		},
		{
			name:                  "not ready -> ready",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionFalse),
			status:                v1alpha1.ConditionTrue,
			message:               "message",
			transitionTimeChanged: true,
		},
		{
			name:                  "ready -> ready",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionTrue),
			status:                v1alpha1.ConditionTrue,
			message:               "message",
			transitionTimeChanged: false,
		},
		{
			name:                  "ready -> not ready",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionTrue),
			status:                v1alpha1.ConditionFalse,
			message:               "message",
			transitionTimeChanged: true,
		},
		{
			name:                  "message -> message2",
			input:                 getTestInstanceWithStatus(v1alpha1.ConditionTrue),
			status:                v1alpha1.ConditionFalse,
			message:               "message2",
			transitionTimeChanged: true,
		},
	}

	for _, tc := range cases {
		_, fakeCatalogClient, fakeBrokerClient, testController, _ := newTestController(t, noFakeActions())

		clone, err := api.Scheme.DeepCopy(tc.input)
		if err != nil {
			t.Errorf("%v: deep copy failed", tc.name)
			continue
		}
		inputClone := clone.(*v1alpha1.ServiceCatalogInstance)

		err = testController.updateInstanceCondition(tc.input, v1alpha1.InstanceConditionReady, tc.status, tc.reason, tc.message)
		if err != nil {
			t.Errorf("%v: error updating instance condition: %v", tc.name, err)
			continue
		}

		brokerActions := fakeBrokerClient.Actions()
		assertNumberOfBrokerActions(t, brokerActions, 0)

		if !reflect.DeepEqual(tc.input, inputClone) {
			t.Errorf("%v: updating broker condition mutated input: expected %v, got %v", tc.name, inputClone, tc.input)
			continue
		}

		actions := fakeCatalogClient.Actions()
		if ok := expectNumberOfActions(t, tc.name, actions, 1); !ok {
			continue
		}

		updatedInstance, ok := expectUpdateStatus(t, tc.name, actions[0], tc.input)
		if !ok {
			continue
		}

		updateActionObject, ok := updatedInstance.(*v1alpha1.ServiceCatalogInstance)
		if !ok {
			t.Errorf("%v: couldn't convert to instance", tc.name)
			continue
		}

		var initialTs metav1.Time
		if len(inputClone.Status.Conditions) != 0 {
			initialTs = inputClone.Status.Conditions[0].LastTransitionTime
		}

		if e, a := 1, len(updateActionObject.Status.Conditions); e != a {
			t.Errorf("%v: expected %v condition(s), got %v", tc.name, e, a)
		}

		outputCondition := updateActionObject.Status.Conditions[0]
		newTs := outputCondition.LastTransitionTime

		if tc.transitionTimeChanged && initialTs == newTs {
			t.Errorf("%v: transition time didn't change when it should have", tc.name)
			continue
		} else if !tc.transitionTimeChanged && initialTs != newTs {
			t.Errorf("%v: transition time changed when it shouldn't have", tc.name)
			continue
		}
		if e, a := tc.reason, outputCondition.Reason; e != "" && e != a {
			t.Errorf("%v: condition reasons didn't match; expected %v, got %v", tc.name, e, a)
			continue
		}
		if e, a := tc.message, outputCondition.Message; e != "" && e != a {
			t.Errorf("%v: condition reasons didn't match; expected %v, got %v", tc.name, e, a)
		}
	}
}
