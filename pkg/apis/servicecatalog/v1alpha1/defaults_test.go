/*
Copyright 2016 The Kubernetes Authors.

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

package v1alpha1_test

import (
	"reflect"
	"testing"

	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog"
	versioned "github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1alpha1"
	"k8s.io/kubernetes/pkg/runtime"
)

func roundTrip(t *testing.T, obj runtime.Object) runtime.Object {
	// TODO: This looks closer to where I need to be... not sure yet how to get
	// the encoder and decoder needed here
	encoder := servicecatalog.Codecs.EncoderForVersion(nil, versioned.SchemeGroupVersion)
	decoder := servicecatalog.Codecs.DecoderToVersion(nil, versioned.SchemeGroupVersion)
	codec := servicecatalog.Codecs.CodecForVersions(encoder, decoder, versioned.SchemeGroupVersion, versioned.SchemeGroupVersion)
	data, err := runtime.Encode(codec, obj)
	if err != nil {
		t.Fatalf("%v\n %#v", err, obj)
	}
	obj2, err := runtime.Decode(codec, data)
	if err != nil {
		t.Errorf("%v\nData: %s\nSource: %#v", err, string(data), obj)
		return nil
	}
	obj3 := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(runtime.Object)
	err = servicecatalog.Scheme.Convert(obj2, obj3, nil)
	if err != nil {
		t.Errorf("%v\nSource: %#v", err, obj2)
		return nil
	}
	return obj3
}

func TestSetDefaultInstance(t *testing.T) {
	i := &versioned.Instance{}
	obj2 := roundTrip(t, runtime.Object(i))
	i2 := obj2.(*versioned.Instance)

	if i2.Spec.OSBGUID == "" {
		t.Error("Expected a default OSBGUID, but got none")
	}
}

func TestSetDefaultBinding(t *testing.T) {
	b := &versioned.Binding{}
	obj2 := roundTrip(t, runtime.Object(b))
	b2 := obj2.(*versioned.Binding)

	if b2.Spec.OSBGUID == "" {
		t.Error("Expected a default OSBGUID, but got none")
	}
}