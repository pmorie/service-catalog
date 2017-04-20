package fake

import (
	api "github.com/openshift/origin/pkg/sdn/api"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeClusterNetworks implements ClusterNetworkInterface
type FakeClusterNetworks struct {
	Fake *FakeCore
	ns   string
}

var clusternetworksResource = schema.GroupVersionResource{Group: "", Version: "", Resource: "clusternetworks"}

func (c *FakeClusterNetworks) Create(clusterNetwork *api.ClusterNetwork) (result *api.ClusterNetwork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(clusternetworksResource, c.ns, clusterNetwork), &api.ClusterNetwork{})

	if obj == nil {
		return nil, err
	}
	return obj.(*api.ClusterNetwork), err
}

func (c *FakeClusterNetworks) Update(clusterNetwork *api.ClusterNetwork) (result *api.ClusterNetwork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(clusternetworksResource, c.ns, clusterNetwork), &api.ClusterNetwork{})

	if obj == nil {
		return nil, err
	}
	return obj.(*api.ClusterNetwork), err
}

func (c *FakeClusterNetworks) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(clusternetworksResource, c.ns, name), &api.ClusterNetwork{})

	return err
}

func (c *FakeClusterNetworks) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(clusternetworksResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &api.ClusterNetworkList{})
	return err
}

func (c *FakeClusterNetworks) Get(name string, options v1.GetOptions) (result *api.ClusterNetwork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(clusternetworksResource, c.ns, name), &api.ClusterNetwork{})

	if obj == nil {
		return nil, err
	}
	return obj.(*api.ClusterNetwork), err
}

func (c *FakeClusterNetworks) List(opts v1.ListOptions) (result *api.ClusterNetworkList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(clusternetworksResource, c.ns, opts), &api.ClusterNetworkList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &api.ClusterNetworkList{}
	for _, item := range obj.(*api.ClusterNetworkList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested clusterNetworks.
func (c *FakeClusterNetworks) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(clusternetworksResource, c.ns, opts))

}

// Patch applies the patch and returns the patched clusterNetwork.
func (c *FakeClusterNetworks) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *api.ClusterNetwork, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(clusternetworksResource, c.ns, name, data, subresources...), &api.ClusterNetwork{})

	if obj == nil {
		return nil, err
	}
	return obj.(*api.ClusterNetwork), err
}
