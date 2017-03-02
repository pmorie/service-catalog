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

package wip

import (
	"encoding/json"
	"testing"

	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1alpha1"
	"github.com/kubernetes-incubator/service-catalog/pkg/brokerapi"
	fakebrokerapi "github.com/kubernetes-incubator/service-catalog/pkg/brokerapi/fake"
	servicecatalogclientset "github.com/kubernetes-incubator/service-catalog/pkg/client/clientset_generated/clientset/fake"
	servicecataloginformers "github.com/kubernetes-incubator/service-catalog/pkg/client/informers"
	v1alpha1informers "github.com/kubernetes-incubator/service-catalog/pkg/client/informers/servicecatalog/v1alpha1"

	"k8s.io/client-go/1.5/kubernetes/fake"

	"k8s.io/kubernetes/pkg/api/v1"
	metav1 "k8s.io/kubernetes/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/client/testing/core"
	"k8s.io/kubernetes/pkg/runtime"
)

const (
	brokerGUID       = "BROKERGUID"
	serviceClassGUID = "SCGUID"
	instanceGUID     = "IGUID"
	bindingGUID      = "BGUID"
)

// broker used in most of the tests that need a broker
func getTestBroker() *v1alpha1.Broker {
	return &v1alpha1.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "test-broker"},
		Spec: v1alpha1.BrokerSpec{
			URL:     "https://example.com",
			OSBGUID: brokerGUID,
		},
	}
}

func getTestServiceClass() *v1alpha1.ServiceClass {
	return &v1alpha1.ServiceClass{
		ObjectMeta: v1.ObjectMeta{Name: "test-serviceclass"},
		BrokerName: "test-broker",
		Plans: []v1alpha1.ServicePlan{{
			Name:    "default",
			OSBFree: true,
			OSBGUID: serviceClassGUID,
		}},
	}
}

func getTestCatalog() *brokerapi.Catalog {
	return &brokerapi.Catalog{
		Services: []*brokerapi.Service{
			{
				Name:        "test-service",
				ID:          "12345",
				Description: "a test service",
				Plans: []brokerapi.ServicePlan{
					{
						Name:        "test-plan",
						Free:        true,
						ID:          "34567",
						Description: "a test plan",
					},
				},
			},
		},
	}
}

type instanceParameters struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args"`
}

type bindingParameters struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

func TestReconcileBroker(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerCatalog, _, _, testController, _, stopCh := newTestController(t)
	defer close(stopCh)

	fakeBrokerCatalog.RetCatalog = getTestCatalog()

	testController.reconcileBroker(getTestBroker())

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 2, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// first action should be a create action for a service class
	createAction := actions[0].(core.CreateAction)
	if e, a := "create", createAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}

	createActionObject := createAction.GetObject().(*v1alpha1.ServiceClass)
	if e, a := "test-service", createActionObject.Name; e != a {
		t.Fatalf("Unexpected name of serviceClass created: expected %v, got %v", e, a)
	}

	// second action should be an update action for broker status subresource
	createAction2 := actions[1].(core.CreateAction)
	if e, a := "update", createAction2.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[1]; expected %v, got %v", e, a)
	}

	createActionObject2 := createAction2.GetObject().(*v1alpha1.Broker)
	if e, a := "test-broker", createActionObject2.Name; e != a {
		t.Fatalf("Unexpected name of broker created: expected %v, got %v", e, a)
	}

	// verify no kube resources created
	kubeActions := fakeKubeClient.Actions()
	if e, a := 0, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

}

func TestReconcileInstanceNonExistentServiceClass(t *testing.T) {
	_, fakeCatalogClient, _, _, _, testController, _, stopCh := newTestController(t)
	defer close(stopCh)

	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "nothere",
			PlanName:         "nothere",
			OSBGUID:          instanceGUID,
		},
	}

	testController.reconcileInstance(instance)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// There should only be one action that says it failed because no such class exists.
	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}
	updateActionObject := updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := "test-instance", updateActionObject.Name; e != a {
		t.Fatalf("Unexpected name of instance created: expected %v, got %v", e, a)
	}
	if e, a := 1, len(updateActionObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of conditions: expected %v, got %v", e, a)
	}
	if e, a := "ReferencesNonexistentServiceClass", updateActionObject.Status.Conditions[0].Reason; e != a {
		t.Fatalf("Unexpected condition reason: expected %v, got %v", e, a)
	}
}

func TestReconcileInstanceNonExistentServicePlan(t *testing.T) {
	_, fakeCatalogClient, _, _, _, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "test-serviceclass",
			PlanName:         "nothere",
			OSBGUID:          instanceGUID,
		},
	}

	testController.reconcileInstance(instance)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// There should only be one action that says it failed because no such class exists.
	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}
	updateActionObject := updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := "test-instance", updateActionObject.Name; e != a {
		t.Fatalf("Unexpected name of instance created: expected %v, got %v", e, a)
	}
	if e, a := 1, len(updateActionObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of conditions: expected %v, got %v", e, a)
	}
	if e, a := "ReferencesNonexistentServicePlan", updateActionObject.Status.Conditions[0].Reason; e != a {
		t.Fatalf("Unexpected condition reason: expected %v, got %v", e, a)
	}
}

func TestReconcileInstanceWithParameters(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerCatalog, fakeInstanceClient, _, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	fakeBrokerCatalog.RetCatalog = getTestCatalog()

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance", Namespace: "test-ns"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "test-serviceclass",
			PlanName:         "default",
			OSBGUID:          instanceGUID,
		},
	}
	parameters := instanceParameters{Name: "test-param", Args: make(map[string]string)}
	parameters.Args["first"] = "first-arg"
	parameters.Args["second"] = "second-arg"

	b, err := json.Marshal(parameters)
	if err != nil {
		t.Fatalf("Failed to marshal parameters %v : %v", parameters, err)
	}
	instance.Spec.Parameters = &runtime.RawExtension{Raw: b}

	testController.reconcileInstance(instance)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// verify no kube resources created
	kubeActions := fakeKubeClient.Actions()
	if e, a := 0, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[1]; expected %v, got %v", e, a)
	}

	updateObject := updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := instance.Name, updateObject.Name; e != a {
		t.Fatalf("Unexpected name of serviceClass created: expected %v, got %v", e, a)
	}

	if e, a := 1, len(updateObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of status conditions: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.InstanceConditionReady, updateObject.Status.Conditions[0].Type; e != a {
		t.Fatalf("Unexpected condition type: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.ConditionTrue, updateObject.Status.Conditions[0].Status; e != a {
		t.Fatalf("Unexpected condition status: expected %v, got %v", e, a)
	}

	// Verify parameters are what we'd expect them to be, basically name, map with two values in it.
	if len(updateObject.Spec.Parameters.Raw) == 0 {
		t.Fatalf("Parameters was unexpectedly empty")
	}
	if si, ok := fakeInstanceClient.Instances[instanceGUID]; !ok {
		t.Fatalf("Did not find the created Instance in fakeInstanceClient after creation")
	} else {
		if len(si.Parameters) == 0 {
			t.Fatalf("Expected parameters but got none")
		}
		if e, a := "test-param", si.Parameters["name"].(string); e != a {
			t.Fatalf("Unexpected name for parameters: expected %v, got %v", e, a)
		}
		argsMap := si.Parameters["args"].(map[string]interface{})
		if e, a := "first-arg", argsMap["first"].(string); e != a {
			t.Fatalf("Unexpected value in parameter map: expected %v, got %v", e, a)
		}
		if e, a := "second-arg", argsMap["second"].(string); e != a {
			t.Fatalf("Unexpected value in parameter map: expected %v, got %v", e, a)
		}
	}
}

func TestReconcileInstance(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, fakeBrokerCatalog, fakeInstanceClient, _, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	fakeBrokerCatalog.RetCatalog = getTestCatalog()

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance", Namespace: "test-ns"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "test-serviceclass",
			PlanName:         "default",
			OSBGUID:          instanceGUID,
		},
	}

	testController.reconcileInstance(instance)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// verify no kube resources created
	kubeActions := fakeKubeClient.Actions()
	if e, a := 0, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[1]; expected %v, got %v", e, a)
	}

	updateObject := updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := instance.Name, updateObject.Name; e != a {
		t.Fatalf("Unexpected name of serviceClass created: expected %v, got %v", e, a)
	}

	if e, a := 1, len(updateObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of status conditions: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.InstanceConditionReady, updateObject.Status.Conditions[0].Type; e != a {
		t.Fatalf("Unexpected condition type: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.ConditionTrue, updateObject.Status.Conditions[0].Status; e != a {
		t.Fatalf("Unexpected condition status: expected %v, got %v", e, a)
	}

	if si, ok := fakeInstanceClient.Instances[instanceGUID]; !ok {
		t.Fatalf("Did not find the created Instance in fakeInstanceClient after creation")
	} else {
		if len(si.Parameters) > 0 {
			t.Fatalf("Unexpected parameters, expected none, got %+v", si.Parameters)
		}
	}
}

func TestReconcileInstanceDelete(t *testing.T) {
	fakeKubeClient, fakeCatalogClient, _, fakeInstanceClient, _, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	fakeInstanceClient.Instances = map[string]*brokerapi.ServiceInstance{
		instanceGUID: &brokerapi.ServiceInstance{},
	}

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())

	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{
			Name:              "test-instance",
			Namespace:         "test-ns",
			DeletionTimestamp: &metav1.Time{},
			Finalizers:        []string{"kubernetes"},
		},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "test-serviceclass",
			PlanName:         "default",
			OSBGUID:          instanceGUID,
		},
	}

	testController.reconcileInstance(instance)

	// Verify no core kube actions occurred
	kubeActions := fakeKubeClient.Actions()
	if e, a := 0, len(kubeActions); e != a {
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	actions := filterActions(fakeCatalogClient.Actions())
	// The two actions should be:
	// 0. Updating the ready condition
	// 1. Removing the finalizer
	if e, a := 2, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}

	updatedObject := updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := instance.Name, updatedObject.Name; e != a {
		t.Fatalf("Unexpected name of instance: expected %v, got %v", e, a)
	}

	if e, a := 1, len(updatedObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of status conditions: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.InstanceConditionReady, updatedObject.Status.Conditions[0].Type; e != a {
		t.Fatalf("Unexpected condition type: expected %v, got %v", e, a)
	}

	if e, a := v1alpha1.ConditionFalse, updatedObject.Status.Conditions[0].Status; e != a {
		t.Fatalf("Unexpected condition status: expected %v, got %v", e, a)
	}

	if _, ok := fakeInstanceClient.Instances[instanceGUID]; ok {
		t.Fatalf("Found the deleted Instance in fakeInstanceClient after deletion")
	}

	updateAction = actions[1].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[1]; expected %v, got %v", e, a)
	}

	updatedObject = updateAction.GetObject().(*v1alpha1.Instance)
	if e, a := instance.Name, updatedObject.Name; e != a {
		t.Fatalf("Unexpected name of instance: expected %v, got %v", e, a)
	}

	if e, a := 0, len(updatedObject.Finalizers); e != a {
		t.Fatalf("Unexpected number of finalizers: expected %v, got %v", e, a)
	}
}

func TestReconcileBindingNonExistingInstance(t *testing.T) {
	_, fakeCatalogClient, _, _, _, testController, _, stopCh := newTestController(t)
	defer close(stopCh)

	binding := &v1alpha1.Binding{
		ObjectMeta: v1.ObjectMeta{Name: "test-binding"},
		Spec: v1alpha1.BindingSpec{
			InstanceRef: v1.ObjectReference{Name: "nothere"},
			OSBGUID:     bindingGUID,
		},
	}

	testController.reconcileBinding(binding)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// There should only be one action that says it failed because no such instance exists.
	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}
	updateActionObject := updateAction.GetObject().(*v1alpha1.Binding)
	if e, a := "test-binding", updateActionObject.Name; e != a {
		t.Fatalf("Unexpected name of binding created: expected %v, got %v", e, a)
	}
	if e, a := 1, len(updateActionObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of conditions: expected %v, got %v", e, a)
	}
	if e, a := "ReferencesNonexistentInstance", updateActionObject.Status.Conditions[0].Reason; e != a {
		t.Fatalf("Unexpected condition reason: expected %v, got %v", e, a)
	}
}

func TestReconcileBindingNonExistingServiceClass(t *testing.T) {
	_, fakeCatalogClient, fakeBrokerCatalog, _, _, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	fakeBrokerCatalog.RetCatalog = getTestCatalog()

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())
	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance", Namespace: "test-ns"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "nothere",
			PlanName:         "default",
			OSBGUID:          instanceGUID,
		},
	}
	sharedInformers.Instances().Informer().GetStore().Add(instance)

	binding := &v1alpha1.Binding{
		ObjectMeta: v1.ObjectMeta{Name: "test-binding", Namespace: "test-ns"},
		Spec: v1alpha1.BindingSpec{
			InstanceRef: v1.ObjectReference{Name: "test-instance", Namespace: "test-ns"},
			OSBGUID:     bindingGUID,
		},
	}

	testController.reconcileBinding(binding)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// There should only be one action that says it failed because no such service class.
	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}
	updateActionObject := updateAction.GetObject().(*v1alpha1.Binding)
	if e, a := "test-binding", updateActionObject.Name; e != a {
		t.Fatalf("Unexpected name of binding created: expected %v, got %v", e, a)
	}
	if e, a := 1, len(updateActionObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of conditions: expected %v, got %v", e, a)
	}
	if e, a := "ReferencesNonexistentServiceClass", updateActionObject.Status.Conditions[0].Reason; e != a {
		t.Fatalf("Unexpected condition reason: expected %v, got %v", e, a)
	}
}

func TestReconcileBindingWithParameters(t *testing.T) {
	_, fakeCatalogClient, fakeBrokerCatalog, _, fakeBindingClient, testController, sharedInformers, stopCh := newTestController(t)
	defer close(stopCh)

	fakeBrokerCatalog.RetCatalog = getTestCatalog()

	sharedInformers.Brokers().Informer().GetStore().Add(getTestBroker())
	sharedInformers.ServiceClasses().Informer().GetStore().Add(getTestServiceClass())
	instance := &v1alpha1.Instance{
		ObjectMeta: v1.ObjectMeta{Name: "test-instance", Namespace: "test-ns"},
		Spec: v1alpha1.InstanceSpec{
			ServiceClassName: "test-serviceclass",
			PlanName:         "default",
			OSBGUID:          instanceGUID,
		},
	}
	sharedInformers.Instances().Informer().GetStore().Add(instance)

	binding := &v1alpha1.Binding{
		ObjectMeta: v1.ObjectMeta{Name: "test-binding", Namespace: "test-ns"},
		Spec: v1alpha1.BindingSpec{
			InstanceRef: v1.ObjectReference{Name: "test-instance", Namespace: "test-ns"},
			OSBGUID:     bindingGUID,
		},
	}

	parameters := bindingParameters{Name: "test-param"}
	parameters.Args = append(parameters.Args, "first-arg")
	parameters.Args = append(parameters.Args, "second-arg")
	b, err := json.Marshal(parameters)
	if err != nil {
		t.Fatalf("Failed to marshal parameters %v : %v", parameters, err)
	}
	binding.Spec.Parameters = &runtime.RawExtension{Raw: b}

	testController.reconcileBinding(binding)

	actions := filterActions(fakeCatalogClient.Actions())
	if e, a := 1, len(actions); e != a {
		t.Logf("%+v\n", actions)
		t.Fatalf("Unexpected number of actions: expected %v, got %v", e, a)
	}

	// There should only be one action that says binding was created
	updateAction := actions[0].(core.UpdateAction)
	if e, a := "update", updateAction.GetVerb(); e != a {
		t.Fatalf("Unexpected verb on actions[0]; expected %v, got %v", e, a)
	}
	updateObject := updateAction.GetObject().(*v1alpha1.Binding)
	if e, a := "test-binding", updateObject.Name; e != a {
		t.Fatalf("Unexpected name of binding created: expected %v, got %v", e, a)
	}
	if e, a := 1, len(updateObject.Status.Conditions); e != a {
		t.Fatalf("Unexpected number of conditions: expected %v, got %v", e, a)
	}
	if e, a := "InjectedBindResult", updateObject.Status.Conditions[0].Reason; e != a {
		t.Fatalf("Unexpected condition reason: expected %v, got %v", e, a)
	}

	// Verify parameters are what we'd expect them to be, basically name, array with two values in it.
	if len(updateObject.Spec.Parameters.Raw) == 0 {
		t.Fatalf("Parameters was unexpectedly empty")
	}
	// TODO(vaikas): Implement the storing logic in the fake Binding Client so that it stores
	// something meaningful there. For now, it just stores a struct making the validation a bit
	// wonky.
	if _, ok := fakeBindingClient.Bindings[instanceGUID+":"+bindingGUID]; !ok {
		t.Fatalf("Did not find the created Binding in fakeInstanceBinding after creation")
	}
}

// newTestController creates a new test controller injected with fake clients
// and returns:
//
// - a fake kubernetes core api client
// - a fake service catalog api client
// - a fake broker catalog client
// - a fake broker instance client
// - a fake broker binding client
// - a test controller
// - the shared informers for the service catalog v1alpha1 api
// - a stop channel hooked to the informer factory that was created
//
// If there is an error, newTestController calls 'Fatal' on the injected
// testing.T.
func newTestController(t *testing.T) (
	*fake.Clientset,
	*servicecatalogclientset.Clientset,
	*fakebrokerapi.CatalogClient,
	*fakebrokerapi.InstanceClient,
	*fakebrokerapi.BindingClient,
	*controller,
	v1alpha1informers.Interface,
	chan struct{}) {
	// create a fake kube client
	fakeKubeClient := &fake.Clientset{}
	// create a fake sc client
	fakeCatalogClient := &servicecatalogclientset.Clientset{}

	catalogCl := &fakebrokerapi.CatalogClient{}
	instanceCl := fakebrokerapi.NewInstanceClient()
	bindingCl := fakebrokerapi.NewBindingClient()
	brokerClFunc := fakebrokerapi.NewClientFunc(catalogCl, instanceCl, bindingCl)

	// create informers
	informerFactory := servicecataloginformers.NewSharedInformerFactory(nil, fakeCatalogClient, 0)
	serviceCatalogSharedInformers := informerFactory.Servicecatalog().V1alpha1()

	// create a test controller
	testController, err := NewController(
		fakeKubeClient,
		fakeCatalogClient.ServicecatalogV1alpha1(),
		serviceCatalogSharedInformers.Brokers(),
		serviceCatalogSharedInformers.ServiceClasses(),
		serviceCatalogSharedInformers.Instances(),
		serviceCatalogSharedInformers.Bindings(),
		brokerClFunc,
	)
	if err != nil {
		t.Fatal(err)
	}

	stopCh := make(chan struct{})
	informerFactory.Start(stopCh)

	return fakeKubeClient, fakeCatalogClient, catalogCl, instanceCl, bindingCl, testController.(*controller), serviceCatalogSharedInformers, stopCh
}

// filterActions filters the list/watch actions on service catalog resources
// from an array of core.Action.  This is so that we can write tests without
// worrying about the list/watching that the informer infrastructure might
// have done.
func filterActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if len(action.GetNamespace()) == 0 &&
			(action.Matches("list", "brokers") ||
				action.Matches("list", "serviceclasses") ||
				action.Matches("list", "instances") ||
				action.Matches("list", "bindings") ||
				action.Matches("watch", "brokers") ||
				action.Matches("watch", "serviceclasses") ||
				action.Matches("watch", "instances") ||
				action.Matches("watch", "bindings")) {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}
