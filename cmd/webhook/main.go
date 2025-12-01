// Package main implements the KubeVirt VM Feature Manager webhook server.
// This mutating admission webhook modifies VirtualMachine objects based on
// feature annotations to enable capabilities like nested virtualization,
// vBIOS injection, PCI passthrough, and GPU device plugins.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/webhook"
)

var (
	scheme = runtime.NewScheme()

	// Version information - set by GoReleaser at build time
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	_ = kubevirtv1.AddToScheme(scheme)
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var showVersion bool
	var port int
	var certDir string
	var errorHandling string
	var logLevel string
	var configSource string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&showVersion, "version", false, "Show version information and exit.")
	flag.IntVar(&port, "port", 0, "The port the webhook server binds to (overrides PORT env var).")
	flag.StringVar(&certDir, "cert-dir", "", "The directory containing TLS certificates (overrides CERT_DIR env var).")
	flag.StringVar(&errorHandling, "error-handling", "", "Error handling mode: 'reject' or 'allow' (overrides ERROR_HANDLING_MODE env var).")
	flag.StringVar(&logLevel, "log-level", "", "Log level: 'debug', 'info', 'warn', 'error' (overrides LOG_LEVEL env var).")
	flag.StringVar(&configSource, "config-source", "", "Configuration source: 'annotations' or 'labels' (overrides CONFIG_SOURCE env var).")
	flag.Parse()

	// Show version and exit if requested
	if showVersion {
		fmt.Printf("vm-feature-manager %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Load configuration first to get defaults
	cfg := config.LoadConfig()

	// Override config with command-line flags if provided
	if port != 0 {
		cfg.Port = port
	}
	if certDir != "" {
		cfg.CertDir = certDir
	}
	if errorHandling != "" {
		cfg.ErrorHandlingMode = errorHandling
	}
	if logLevel != "" {
		cfg.LogLevel = logLevel
	}
	if configSource != "" {
		if !utils.IsValidConfigSource(configSource) {
			fmt.Fprintf(os.Stderr, "Invalid config-source value: %s (must be 'annotations' or 'labels')\n", configSource)
			os.Exit(1)
		}
		cfg.ConfigSource = configSource
	}

	// Set up logger with configured log level
	zapOpts := []zap.Opts{}
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		zapOpts = append(zapOpts, zap.UseDevMode(true), zap.Level(zapcore.DebugLevel))
	case "info":
		zapOpts = append(zapOpts, zap.UseDevMode(false), zap.Level(zapcore.InfoLevel))
	case "warn", "warning":
		zapOpts = append(zapOpts, zap.UseDevMode(false), zap.Level(zapcore.WarnLevel))
	case "error":
		zapOpts = append(zapOpts, zap.UseDevMode(false), zap.Level(zapcore.ErrorLevel))
	default:
		zapOpts = append(zapOpts, zap.UseDevMode(false), zap.Level(zapcore.InfoLevel))
	}
	log.SetLogger(zap.New(zapOpts...))
	logger := log.Log.WithName("vm-feature-manager")
	ctx := log.IntoContext(context.Background(), logger)

	logger.Info("Starting VM Feature Manager Webhook",
		"version", version,
		"commit", commit,
		"buildDate", date)

	logger.Info("Configuration loaded",
		"port", cfg.Port,
		"logLevel", cfg.LogLevel,
		"errorHandlingMode", cfg.ErrorHandlingMode,
		"configSource", cfg.ConfigSource)

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
		features.NewNestedVirtualization(&cfg.Features.NestedVirtualization, cfg.ConfigSource),
		features.NewPciPassthrough(cfg.ConfigSource),
		features.NewVBiosInjection(cfg.ConfigSource),
		features.NewGpuDevicePlugin(cfg.ConfigSource),
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
