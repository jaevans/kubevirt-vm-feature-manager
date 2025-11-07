// Package main implements the KubeVirt VM Feature Manager webhook server.
// This mutating admission webhook modifies VirtualMachine objects based on
// feature annotations to enable capabilities like nested virtualization,
// vBIOS injection, PCI passthrough, and GPU device plugins.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/webhook"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = kubevirtv1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.Parse()

	// Set up logger
	log.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := log.Log.WithName("vm-feature-manager")
	ctx := log.IntoContext(context.Background(), logger)

	logger.Info("Starting VM Feature Manager Webhook")

	// Load configuration
	cfg := config.LoadConfig()
	logger.Info("Configuration loaded",
		"port", cfg.Port,
		"logLevel", cfg.LogLevel,
		"errorHandlingMode", cfg.ErrorHandlingMode)

	// Create Kubernetes client
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		logger.Error(err, "Failed to get Kubernetes config")
		os.Exit(1)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "Failed to create Kubernetes client")
		os.Exit(1)
	}

	// Initialize features
	featureList := []features.Feature{
		features.NewNestedVirtualization(&cfg.Features.NestedVirtualization),
		features.NewPciPassthrough(),
		features.NewVBiosInjection(),
		features.NewGpuDevicePlugin(),
	}

	logger.Info("Features initialized", "count", len(featureList))

	// Create mutator
	mutator := webhook.NewMutator(k8sClient, cfg, featureList)

	// Create handler
	handler := webhook.NewHandler(mutator)

	// Create server
	server := webhook.NewServer(cfg, handler)

	// Set up signal handling
	sigCtx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Start server
	logger.Info("Starting webhook server", "port", cfg.Port)
	if err := server.Start(sigCtx); err != nil {
		logger.Error(err, "Failed to start webhook server")
		os.Exit(1)
	}

	logger.Info("Webhook server stopped gracefully")
}
