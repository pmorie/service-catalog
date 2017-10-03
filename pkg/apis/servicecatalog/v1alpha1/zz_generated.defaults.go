// +build !ignore_autogenerated

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

// This file was autogenerated by defaulter-gen. Do not edit it manually!

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// RegisterDefaults adds defaulters functions to the given scheme.
// Public to allow building arbitrary schemes.
// All generated defaulters are covering - they call all nested defaulters.
func RegisterDefaults(scheme *runtime.Scheme) error {
	scheme.AddTypeDefaultingFunc(&ClusterServiceBroker{}, func(obj interface{}) { SetObjectDefaults_ClusterServiceBroker(obj.(*ClusterServiceBroker)) })
	scheme.AddTypeDefaultingFunc(&ClusterServiceBrokerList{}, func(obj interface{}) { SetObjectDefaults_ClusterServiceBrokerList(obj.(*ClusterServiceBrokerList)) })
	scheme.AddTypeDefaultingFunc(&ServiceInstance{}, func(obj interface{}) { SetObjectDefaults_ServiceInstance(obj.(*ServiceInstance)) })
	scheme.AddTypeDefaultingFunc(&ServiceInstanceCredential{}, func(obj interface{}) { SetObjectDefaults_ServiceInstanceCredential(obj.(*ServiceInstanceCredential)) })
	scheme.AddTypeDefaultingFunc(&ServiceInstanceCredentialList{}, func(obj interface{}) {
		SetObjectDefaults_ServiceInstanceCredentialList(obj.(*ServiceInstanceCredentialList))
	})
	scheme.AddTypeDefaultingFunc(&ServiceInstanceList{}, func(obj interface{}) { SetObjectDefaults_ServiceInstanceList(obj.(*ServiceInstanceList)) })
	return nil
}

func SetObjectDefaults_ClusterServiceBroker(in *ClusterServiceBroker) {
	SetDefaults_ClusterServiceBrokerSpec(&in.Spec)
}

func SetObjectDefaults_ClusterServiceBrokerList(in *ClusterServiceBrokerList) {
	for i := range in.Items {
		a := &in.Items[i]
		SetObjectDefaults_ClusterServiceBroker(a)
	}
}

func SetObjectDefaults_ServiceInstance(in *ServiceInstance) {
	SetDefaults_ServiceInstanceSpec(&in.Spec)
}

func SetObjectDefaults_ServiceInstanceCredential(in *ServiceInstanceCredential) {
	SetDefaults_ServiceInstanceCredential(in)
	SetDefaults_ServiceInstanceCredentialSpec(&in.Spec)
}

func SetObjectDefaults_ServiceInstanceCredentialList(in *ServiceInstanceCredentialList) {
	for i := range in.Items {
		a := &in.Items[i]
		SetObjectDefaults_ServiceInstanceCredential(a)
	}
}

func SetObjectDefaults_ServiceInstanceList(in *ServiceInstanceList) {
	for i := range in.Items {
		a := &in.Items[i]
		SetObjectDefaults_ServiceInstance(a)
	}
}
