/*
Copyright 2018 The Knative Authors

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

package main

import (
	"flag"
	"log"
	"time"

	certmanagerclientset "github.com/jetstack/cert-manager/pkg/client/clientset/versioned"
	certmanagerinformers "github.com/jetstack/cert-manager/pkg/client/informers/externalversions"
	"github.com/knative/pkg/configmap"
	"github.com/knative/pkg/controller"
	"github.com/knative/pkg/signals"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	informers "github.com/knative/serving/pkg/client/informers/externalversions"
	"github.com/knative/serving/pkg/logging"
	"github.com/knative/serving/pkg/reconciler"
	tlsprovision "github.com/knative/serving/pkg/reconciler/v1alpha1/tlsprovision/certmanager"
	"github.com/knative/serving/pkg/system"
	"go.uber.org/zap"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	threadsPerController = 1
	logLevelKey          = "tlsprovisioner"
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func main() {
	flag.Parse()

	loggingConfigMap, err := configmap.Load("/etc/config-logging")
	if err != nil {
		log.Fatalf("Error loading logging configuration: %v", err)
	}
	loggingConfig, err := logging.NewConfigFromMap(loggingConfigMap)
	if err != nil {
		log.Fatalf("Error parsing logging configuration: %v", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, logLevelKey)
	defer logger.Sync()

	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		logger.Fatal("Error building kubeconfig.", zap.Error(err))
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Error building kubernetes clientset.", zap.Error(err))
	}

	servingClient, err := clientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building serving clientset: %v", err)
	}

	certManagerClient, err := certmanagerclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building cert manager clientset: %v", err)
	}

	configMapWatcher := configmap.NewInformedWatcher(kubeClient, system.Namespace)

	opt := reconciler.Options{
		KubeClientSet:    kubeClient,
		ServingClientSet: servingClient,
		ConfigMapWatcher: configMapWatcher,
		Logger:           logger,
		ResyncPeriod:     10 * time.Hour, // Based on controller-runtime default.
		StopChannel:      stopCh,
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, opt.ResyncPeriod)
	servingInformerFactory := informers.NewSharedInformerFactory(servingClient, opt.ResyncPeriod)
	certmanagerInformerFactory := certmanagerinformers.NewSharedInformerFactory(certManagerClient, opt.ResyncPeriod)

	routeInformer := servingInformerFactory.Serving().V1alpha1().Routes()
	certificateInformer := certmanagerInformerFactory.Certmanager().V1alpha1().Certificates()

	logger.Info("Creating tls provisioner.")
	certmanagerTLSProvisioner := tlsprovision.NewController(
		opt,
		routeInformer,
		certificateInformer,
		certManagerClient,
	)

	logger.Info("TLS provisioner created.")

	// Watch the logging config map and dynamically update logging levels.
	configMapWatcher.Watch(logging.ConfigName, logging.UpdateLevelFromConfigMap(logger, atomicLevel, logLevelKey))

	// These are non-blocking.
	kubeInformerFactory.Start(stopCh)
	servingInformerFactory.Start(stopCh)
	certmanagerInformerFactory.Start(stopCh)
	if err := configMapWatcher.Start(stopCh); err != nil {
		logger.Fatalf("failed to start configuration manager: %v", err)
	}

	// Wait for the caches to be synced before starting controllers.
	logger.Info("Waiting for informer caches to sync")
	for i, synced := range []cache.InformerSynced{
		routeInformer.Informer().HasSynced,
		certificateInformer.Informer().HasSynced,
	} {
		logger.Info("syncing cache.")
		if ok := cache.WaitForCacheSync(stopCh, synced); !ok {
			logger.Fatalf("failed to wait for cache at index %v to sync", i)
		}
		logger.Info("cache synced.")
	}

	logger.Info("starting controller.")

	// Start all of the controllers.
	go func(ctrlr *controller.Impl) {
		logger.Info("Starting tlsprovision controller.")
		// We don't expect this to return until stop is called,
		// but if it does, propagate it back.
		if runErr := ctrlr.Run(threadsPerController, stopCh); runErr != nil {
			logger.Fatalf("Error running controller: %v", runErr)
		}
	}(certmanagerTLSProvisioner)

	logger.Info("controller is started.")

	<-stopCh
}
