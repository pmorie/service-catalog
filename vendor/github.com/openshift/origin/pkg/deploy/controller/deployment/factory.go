package deployment

import (
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kv1core "k8s.io/client-go/kubernetes/typed/core/v1"
	kclientv1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	kapi "k8s.io/kubernetes/pkg/api"
	kclientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	kcoreinformers "k8s.io/kubernetes/pkg/client/informers/informers_generated/internalversion/core/internalversion"
	kcontroller "k8s.io/kubernetes/pkg/controller"

	deployutil "github.com/openshift/origin/pkg/deploy/util"
)

const (
	// We must avoid creating processing deployment configs until the deployment config and image
	// stream stores have synced. If it hasn't synced, to avoid a hot loop, we'll wait this long
	// between checks.
	storeSyncedPollPeriod = 100 * time.Millisecond
)

// NewDeploymentController creates a new DeploymentController.
func NewDeploymentController(
	rcInformer kcoreinformers.ReplicationControllerInformer,
	podInformer kcoreinformers.PodInformer,
	kc kclientset.Interface,
	sa,
	image string,
	env []kapi.EnvVar,
	codec runtime.Codec,
) *DeploymentController {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&kv1core.EventSinkImpl{Interface: kv1core.New(kc.Core().RESTClient()).Events("")})
	recorder := eventBroadcaster.NewRecorder(kapi.Scheme, kclientv1.EventSource{Component: "deployments-controller"})

	c := &DeploymentController{
		rn: kc.Core(),
		pn: kc.Core(),

		queue: workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),

		rcLister:        rcInformer.Lister(),
		rcListerSynced:  rcInformer.Informer().HasSynced,
		podLister:       podInformer.Lister(),
		podListerSynced: podInformer.Informer().HasSynced,

		serviceAccount: sa,
		deployerImage:  image,
		environment:    env,
		recorder:       recorder,
		codec:          codec,
	}

	rcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addReplicationController,
		UpdateFunc: c.updateReplicationController,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.updatePod,
		DeleteFunc: c.deletePod,
	})

	return c
}

// Run begins watching and syncing.
func (c *DeploymentController) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	glog.Infof("Starting deployment controller")

	// Wait for the dc store to sync before starting any work in this controller.
	if !cache.WaitForCacheSync(stopCh, c.rcListerSynced, c.podListerSynced) {
		return
	}

	glog.Infof("Deployment controller caches are synced. Starting workers.")

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}

	<-stopCh

	glog.Infof("Shutting down deployment controller")
}

func (c *DeploymentController) addReplicationController(obj interface{}) {
	rc := obj.(*kapi.ReplicationController)
	// Filter out all unrelated replication controllers.
	if !deployutil.IsOwnedByConfig(rc) {
		return
	}

	c.enqueueReplicationController(rc)
}

func (c *DeploymentController) updateReplicationController(old, cur interface{}) {
	// A periodic relist will send update events for all known controllers.
	curRC := cur.(*kapi.ReplicationController)
	oldRC := old.(*kapi.ReplicationController)
	if curRC.ResourceVersion == oldRC.ResourceVersion {
		return
	}

	// Filter out all unrelated replication controllers.
	if !deployutil.IsOwnedByConfig(curRC) {
		return
	}

	c.enqueueReplicationController(curRC)
}

func (c *DeploymentController) updatePod(old, cur interface{}) {
	// A periodic relist will send update events for all known pods.
	curPod := cur.(*kapi.Pod)
	oldPod := old.(*kapi.Pod)
	if curPod.ResourceVersion == oldPod.ResourceVersion {
		return
	}

	if rc, err := c.rcForDeployerPod(curPod); err == nil && rc != nil {
		c.enqueueReplicationController(rc)
	}
}

func (c *DeploymentController) deletePod(obj interface{}) {
	pod, ok := obj.(*kapi.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone: %+v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*kapi.Pod)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a pod: %+v", obj)
			return
		}
	}

	if rc, err := c.rcForDeployerPod(pod); err == nil && rc != nil {
		c.enqueueReplicationController(rc)
	}
}

func (c *DeploymentController) enqueueReplicationController(rc *kapi.ReplicationController) {
	key, err := kcontroller.KeyFunc(rc)
	if err != nil {
		glog.Errorf("Couldn't get key for object %#v: %v", rc, err)
		return
	}
	c.queue.Add(key)
}

func (c *DeploymentController) rcForDeployerPod(pod *kapi.Pod) (*kapi.ReplicationController, error) {
	key := pod.Namespace + "/" + deployutil.DeploymentNameFor(pod)
	return c.getByKey(key)
}

func (c *DeploymentController) worker() {
	for {
		if quit := c.work(); quit {
			return
		}
	}
}

func (c *DeploymentController) work() bool {
	key, quit := c.queue.Get()
	if quit {
		return true
	}
	defer c.queue.Done(key)

	rc, err := c.getByKey(key.(string))
	if err != nil {
		glog.Error(err.Error())
	}

	if rc == nil {
		return false
	}

	// Resist missing deployer pods from the cache in case of a pending deployment.
	// Give some room for a possible rc update failure in case we decided to mark it
	// failed.
	willBeDropped := c.queue.NumRequeues(key) >= maxRetryCount-2
	err = c.handle(rc, willBeDropped)
	c.handleErr(err, key, rc)

	return false
}

func (c *DeploymentController) getByKey(key string) (*kapi.ReplicationController, error) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}
	rc, err := c.rcLister.ReplicationControllers(namespace).Get(name)
	if errors.IsNotFound(err) {
		glog.Infof("Replication controller %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		glog.Infof("Unable to retrieve replication controller %q from store: %v", key, err)
		c.queue.Add(key)
		return nil, err
	}

	return rc, nil
}