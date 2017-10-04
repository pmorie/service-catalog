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
	"github.com/golang/glog"
	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1alpha1"
	"k8s.io/client-go/tools/cache"
)

// Service plan handlers and control-loop

func (c *controller) servicePlanAdd(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		glog.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	c.servicePlanQueue.Add(key)
}

// reconcileServicePlanKey reconciles a ServicePlan due to resync or an event
// on the ServicePlan.  Note that this is NOT the main reconciliation loop for
// ServicePlans. ServicePlans are primarily reconciled in a separate flow when
// a ClusterServiceBroker is reconciled.
func (c *controller) reconcileServicePlanKey(key string) error {
	// currently, this is a no-op.  In the future, we'll maintain status
	// information here.
	return nil
}

func (c *controller) servicePlanUpdate(oldObj, newObj interface{}) {
	c.servicePlanAdd(newObj)
}

func (c *controller) servicePlanDelete(obj interface{}) {
	servicePlan, ok := obj.(*v1alpha1.ServicePlan)
	if servicePlan == nil || !ok {
		return
	}

	glog.V(4).Infof("Received delete event for ServicePlan %v; no further processing will occur", servicePlan.Name)
}
