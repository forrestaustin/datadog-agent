// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

// +build kubeapiserver

package apiserver

import (
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/util/kubernetes/autoscalers"
	"github.com/DataDog/datadog-agent/pkg/util/log"

	wpa_client "github.com/DataDog/watermarkpodautoscaler/pkg/client/clientset/versioned"
	"github.com/DataDog/watermarkpodautoscaler/pkg/client/informers/externalversions"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

type controllerFuncs struct {
	enabled func() bool
	start   func(ControllerContext) error
}

var controllerCatalog = map[string]controllerFuncs{
	"metadata": {
		func() bool { return config.Datadog.GetBool("kubernetes_collect_metadata_tags") },
		startMetadataController,
	},
	"autoscalers": {
		func() bool { return config.Datadog.GetBool("external_metrics_provider.enabled") },
		startAutoscalersController,
	},
	"services": {
		func() bool { return config.Datadog.GetBool("cluster_checks.enabled") },
		startServicesInformer,
	},
	"endpoints": {
		func() bool { return config.Datadog.GetBool("cluster_checks.enabled") },
		startEndpointsInformer,
	},
}

type ControllerContext struct {
	InformerFactory    informers.SharedInformerFactory
	WPAClient          wpa_client.Interface
	WPAInformerFactory externalversions.SharedInformerFactory
	Client             kubernetes.Interface
	LeaderElector      LeaderElectorInterface
	EventRecorder      record.EventRecorder
	StopCh             chan struct{}
}

// StartControllers runs the enabled Kubernetes controllers for the Datadog Cluster Agent. This is
// only called once, when we have confirmed we could correctly connect to the API server.
func StartControllers(ctx ControllerContext) error {
	for name, cntrlFuncs := range controllerCatalog {
		if !cntrlFuncs.enabled() {
			log.Infof("%q is disabled", name)
			continue
		}
		err := cntrlFuncs.start(ctx)
		if err != nil {
			log.Errorf("Error starting %q: %s", name, err.Error())
		}
	}

	// we must start the informer factory after starting the controllers because the informer
	// factory uses lazy initialization (delays the creation of an informer until the first
	// time it's needed).
	// TODO: If any of the controllers here are initialized asynchronously, relying on the
	// informer factory to run informers for these controllers will not initialize them properly.
	// FIXME: We may want to initialize each of these controllers separately via their respective
	// `<informer>.Run()`
	ctx.InformerFactory.Start(ctx.StopCh)

	return nil
}

// startMetadataController starts the informers needed for metadata collection.
// The synchronization of the informers is handled in this function.
func startMetadataController(ctx ControllerContext) error {
	metaController := NewMetadataController(
		ctx.InformerFactory.Core().V1().Nodes(),
		ctx.InformerFactory.Core().V1().Endpoints(),
	)
	go metaController.Run(ctx.StopCh)

	// Wait for the cache to sync
	return SyncInformers(map[string]cache.SharedInformer{
		"nodes":     ctx.InformerFactory.Core().V1().Nodes().Informer(),
		"endpoints": ctx.InformerFactory.Core().V1().Endpoints().Informer(),
	})
}

// startAutoscalersController starts the informers needed for autoscaling.
// The synchronization of the informers is handled in this function.
func startAutoscalersController(ctx ControllerContext) error {
	dogCl, err := autoscalers.NewDatadogClient()
	if err != nil {
		return err
	}
	autoscalersController, err := NewAutoscalersController(
		ctx.Client,
		ctx.EventRecorder,
		ctx.LeaderElector,
		dogCl,
	)
	if err != nil {
		return err
	}
	informers := map[string]cache.SharedInformer{}
	if ctx.WPAInformerFactory != nil {
		go autoscalersController.RunWPA(ctx.StopCh, ctx.WPAClient, ctx.WPAInformerFactory)
		informers["wpa"] = ctx.WPAInformerFactory.Datadoghq().V1alpha1().WatermarkPodAutoscalers().Informer()
	}
	// mutate the Autoscaler controller to embed an informer against the HPAs
	autoscalersController.EnableHPA(ctx.InformerFactory.Autoscaling().V2beta1().HorizontalPodAutoscalers())
	go autoscalersController.RunHPA(ctx.StopCh)
	informers["hpa"] = ctx.InformerFactory.Autoscaling().V2beta1().HorizontalPodAutoscalers().Informer()

	autoscalersController.RunControllerLoop(ctx.StopCh)

	// Wait for the cache to sync
	return SyncInformers(informers)
}

// startServicesInformer starts the service informer.
// The synchronization of the service informer is handled in this function.
func startServicesInformer(ctx ControllerContext) error {
	// Just start the shared informer, the autodiscovery
	// components will access it when needed.
	go ctx.InformerFactory.Core().V1().Services().Informer().Run(ctx.StopCh)

	// Wait for the cache to sync
	return SyncInformers(map[string]cache.SharedInformer{
		"services": ctx.InformerFactory.Core().V1().Services().Informer(),
	})
}

// startEndpointsInformer starts the endpoints informer.
// The synchronization of the endpoints informer is handled in this function.
func startEndpointsInformer(ctx ControllerContext) error {
	// Just start the shared informer, the autodiscovery
	// components will access it when needed.
	go ctx.InformerFactory.Core().V1().Endpoints().Informer().Run(ctx.StopCh)

	// Wait for the cache to sync
	return SyncInformers(map[string]cache.SharedInformer{
		"endpoints": ctx.InformerFactory.Core().V1().Endpoints().Informer(),
	})
}
