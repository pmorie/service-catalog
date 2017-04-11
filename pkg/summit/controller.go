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

package summit

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"

	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1alpha1"
	servicecatalogclientset "github.com/kubernetes-incubator/service-catalog/pkg/client/clientset_generated/clientset/typed/servicecatalog/v1alpha1"
	informers "github.com/kubernetes-incubator/service-catalog/pkg/client/informers_generated/externalversions/servicecatalog/v1alpha1"

	osclient "github.com/openshift/origin/pkg/client"
)

func NewController(
	kubeClient kubernetes.Interface,
	serviceCatalogClient servicecatalogclientset.ServicecatalogV1alpha1Interface,
	osClient osClient.Interface,
	bindingInformer informers.BindingInformer,
) (Controller, error) {
	controller := &controller{
		kubeClient:           kubeClient,
		serviceCatalogClient: serviceCatalogClient,
		osClient:             osClient,
	}

	bindingInformer.Informer().AddEventHandler(cache.ResourceEventHandleFuncs{
		AddFunc:    controller.bindingAdd,
		UpdateFunc: controller.bindingUpdate,
		DeleteFunc: controller.bindingDelete,
	})

	return controller, nil
}

// Controller describes a summit demo hack controller.
type Controller interface {
	// Run runs the controller until the given stop channel can be read from.
	Run(stopCh <-chan struct{})
}

type controller struct {
	kubeClient           kubernetes.Interface
	serviceCatalogClient servicecatalogclientset.ServicecatalogV1alpha1Interface
	osClient             osclient.Interface
}

func (c *controller) bindingAdd(obj interface{}) {
	binding, ok := obj.(*v1alpha1.Binding)
	if binding == nil || !ok {
		return
	}

	c.reconcileBinding(binding)
}

func (c *controller) bindingUpdate(oldObj, newObj interface{}) {
	c.bindingAdd(newObj)
}

const specialAnnotationKey = "summit.openshift.io/mutated"

func (c *controller) reconcileBinding(binding *v1alpha1.Binding) {
	if len(binding.Status.Conditions) == 0 {
		return
	}

	if binding.Status.Conditions[0].Type != v1alpha1.BrokerConditionReady {
		return
	}

	if binding.Status.Conditions[0].Status != v1alpha1.ConditionTrue {
		return
	}

	ns := binding.Namespace
	secretName := binding.Spec.SecretName

	secret, err := c.kubeClient.Core().Secrets(ns).Get(secretName)
	if err != nil {
		glog.Errorf("Error getting secret %v/%v", ns, secretName)
	}

	deploymentConfigs, err := c.osClient.DeploymentConfigs(ns).List(metav1.ListOptions{})
	if err != nil {
		glog.Errorf("Error getting deployments for namespace %v", ns)
	}

	for _, dc := range deploymentConfigs {
		_, ok := dc.Annotations[specialAnnotationKey]
		if ok {
			// we already did this one, next
			continue
		}

		for key := range secret.Data {
			env := v1.EnvVar{
				Name: key,
				ValueFrom: &v1.SecretKeyRef{
					LocalObjectReference: v1.LocalObjectReference{
						Name: secretName,
					},
					Key: key,
				},
			}

			for _, container := range dc.Spec.Template.Spec.Containers {
				glog.Infof("Adding env %v (from secret %v) to deploymentConfig %v/%v", env.Name, secretName, ns, dc.Name)
				container.Env = append(container.Env, env)
			}
		}

		dc.Annotations[specialAnnotationKey] = "set"

		_, err := c.osClient.DeploymentConfigs(ns).Update(dc)
		if err != nil {
			glog.Errorf("Error updating deploymentConfig %v/%v", ns, dc.Name)
		}
	}
}

func (c *controller) bindingDelete(obj interface{}) {
	binding, ok := obj.(*v1alpha1.Binding)
	if binding == nil || !ok {
		return
	}

	glog.V(4).Infof("Received delete event for Binding %v/%v", binding.Namespace, binding.Name)
}
