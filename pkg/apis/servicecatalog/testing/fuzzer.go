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

package testing

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/google/gofuzz"
	"github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog"
	"github.com/satori/go.uuid"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/apis/extensions"
	metav1 "k8s.io/kubernetes/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/types"
)

// FuzzerFor can randomly populate api objects that are destined for version.
func FuzzerFor(t *testing.T, version schema.GroupVersion, src rand.Source) *fuzz.Fuzzer {
	f := fuzz.New().NilChance(.5).NumElements(0, 1)
	if src != nil {
		f.RandSource(src)
	}
	f.Funcs(
		func(j *int, c fuzz.Continue) {
			*j = int(c.Int31())
		},
		func(j **int, c fuzz.Continue) {
			if c.RandBool() {
				i := int(c.Int31())
				*j = &i
			} else {
				*j = nil
			}
		},
		func(j *runtime.TypeMeta, c fuzz.Continue) {
			// We have to customize the randomization of TypeMetas because their
			// APIVersion and Kind must remain blank in memory.
			j.APIVersion = ""
			j.Kind = ""
		},
		func(j *metav1.TypeMeta, c fuzz.Continue) {
			// We have to customize the randomization of TypeMetas because their
			// APIVersion and Kind must remain blank in memory.
			j.APIVersion = ""
			j.Kind = ""
		},
		func(j *api.ObjectMeta, c fuzz.Continue) {
			j.Name = c.RandString()
			j.ResourceVersion = strconv.FormatUint(c.RandUint64(), 10)
			j.SelfLink = c.RandString()
			j.UID = types.UID(c.RandString())
			j.GenerateName = c.RandString()

			var sec, nsec int64
			c.Fuzz(&sec)
			c.Fuzz(&nsec)
			j.CreationTimestamp = metav1.Unix(sec, nsec).Rfc3339Copy()
		},
		func(j *api.ObjectReference, c fuzz.Continue) {
			// We have to customize the randomization of TypeMetas because their
			// APIVersion and Kind must remain blank in memory.
			j.APIVersion = c.RandString()
			j.Kind = c.RandString()
			j.Namespace = c.RandString()
			j.Name = c.RandString()
			j.ResourceVersion = strconv.FormatUint(c.RandUint64(), 10)
			j.FieldPath = c.RandString()
		},
		func(j *metav1.ListMeta, c fuzz.Continue) {
			j.ResourceVersion = strconv.FormatUint(c.RandUint64(), 10)
			j.SelfLink = c.RandString()
		},
		func(j *runtime.Object, c fuzz.Continue) {
			// TODO: uncomment when round trip starts from a versioned object
			if true { //c.RandBool() {
				*j = &runtime.Unknown{
					// We do not set TypeMeta here because it is not carried through a round trip
					Raw:         []byte(`{"apiVersion":"unknown.group/unknown","kind":"Something","someKey":"someValue"}`),
					ContentType: runtime.ContentTypeJSON,
				}
			} else {
				types := []runtime.Object{&api.Pod{}, &api.ReplicationController{}}
				t := types[c.Rand.Intn(len(types))]
				c.Fuzz(t)
				*j = t
			}
		},
		func(r *runtime.RawExtension, c fuzz.Continue) {
			// Pick an arbitrary type and fuzz it
			types := []runtime.Object{&api.Pod{}, &extensions.Deployment{}, &api.Service{}}
			obj := types[c.Rand.Intn(len(types))]
			c.Fuzz(obj)

			// Find a codec for converting the object to raw bytes.  This is necessary for the
			// api version and kind to be correctly set be serialization.
			var codec runtime.Codec
			switch obj.(type) {
			case *api.Pod:
				codec = testapi.Default.Codec()
			case *extensions.Deployment:
				codec = testapi.Extensions.Codec()
			case *api.Service:
				codec = testapi.Default.Codec()
			default:
				t.Errorf("Failed to find codec for object type: %T", obj)
				return
			}

			// Convert the object to raw bytes
			bytes, err := runtime.Encode(codec, obj)
			if err != nil {
				t.Errorf("Failed to encode object: %v", err)
				return
			}

			// Set the bytes field on the RawExtension
			r.Raw = bytes
		},
		func(i *servicecatalog.Instance, c fuzz.Continue) {
			c.FuzzNoCustom(i)
			i.Spec.OSBGUID = uuid.NewV4().String()
		},
		func(b *servicecatalog.Binding, c fuzz.Continue) {
			c.FuzzNoCustom(b)
			b.Spec.OSBGUID = uuid.NewV4().String()
		},
	)
	return f
}
