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

package integration

import (
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/golang/glog"
	"github.com/pborman/uuid"

	"k8s.io/client-go/pkg/api"
	restclient "k8s.io/client-go/rest"

	genericserveroptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/storage/storagebackend"

	"github.com/kubernetes-incubator/service-catalog/cmd/apiserver/app/server"
	_ "github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/install"
	servicecatalogclient "github.com/kubernetes-incubator/service-catalog/pkg/client/clientset_generated/clientset"
	_ "k8s.io/client-go/pkg/api/install"
	_ "k8s.io/client-go/pkg/apis/extensions/install"
)

const (
	globalTPRNamespace = "globalTPRNamespace"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func getFreshApiserverAndClient(t *testing.T, storageTypeStr string) (servicecatalogclient.Interface, func()) {
	securePort := rand.Intn(35534) + 3000
	serverIP := fmt.Sprintf("https://localhost:%d", securePort)
	stopCh := make(chan struct{})
	serverFailed := make(chan struct{})
	shutdown := func() {
		t.Logf("Shutting down server on port: %d", securePort)
		close(stopCh)
	}

	certDir, _ := ioutil.TempDir("", "service-catalog-integration")

	secureServingOptions := genericserveroptions.NewSecureServingOptions()
	go func() {

		tprOptions := server.NewTPROptions()
		tprOptions.RESTClient = getFakeCoreRESTClient()
		tprOptions.InstallTPRsFunc = func() error {
			return nil
		}
		tprOptions.GlobalNamespace = globalTPRNamespace
		options := &server.ServiceCatalogServerOptions{
			StorageTypeString:       storageTypeStr,
			GenericServerRunOptions: genericserveroptions.NewServerRunOptions(),
			SecureServingOptions:    secureServingOptions,
			InsecureServingOptions:  genericserveroptions.NewInsecureServingOptions(),
			EtcdOptions: &server.EtcdOptions{
				EtcdOptions: genericserveroptions.NewEtcdOptions(storagebackend.NewDefaultConfig(uuid.New(), api.Scheme, nil)),
			},
			TPROptions:            tprOptions,
			AuthenticationOptions: genericserveroptions.NewDelegatingAuthenticationOptions(),
			AuthorizationOptions:  genericserveroptions.NewDelegatingAuthorizationOptions(),
			StopCh:                stopCh,
		}
		options.SecureServingOptions.ServingOptions.BindPort = securePort
		options.SecureServingOptions.ServerCert.CertDirectory = certDir
		options.EtcdOptions.StorageConfig.ServerList = []string{"http://localhost:2379"}
		if err := server.RunServer(options); err != nil {
			close(serverFailed)
			t.Fatalf("Error in bringing up the server: %v", err)
		}
	}()

	if err := waitForApiserverUp(serverIP, serverFailed); err != nil {
		t.Fatalf("%v", err)
	}

	config := &restclient.Config{}
	config.Host = serverIP
	config.Insecure = true
	clientset, err := servicecatalogclient.NewForConfig(config)
	if nil != err {
		t.Fatal("can't make the client from the config", err)
	}
	return clientset, shutdown
}

func waitForApiserverUp(serverIP string, stopCh <-chan struct{}) error {
	minuteTimeout := time.After(2 * time.Minute)
	for {
		select {
		case <-stopCh:
			return fmt.Errorf("apiserver failed")
		case <-minuteTimeout:
			return fmt.Errorf("waiting for apiserver timed out")
		default:
			glog.Infof("Waiting for : %#v", serverIP)
			tr := &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
			client := &http.Client{Transport: tr}
			_, err := client.Get(serverIP)
			if err == nil {
				return nil
			}
		}
		// no success or overall timeout or stop due to failure
		// wait and go around again
		<-time.After(10 * time.Second)
	}
}
