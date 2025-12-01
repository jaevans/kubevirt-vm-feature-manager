// Package config provides configuration management for the VM Feature Manager webhook.
package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

// Config holds the webhook configuration
type Config struct {
	// Server configuration
	Port    int
	CertDir string

	// Logging
	LogLevel string

	// Error handling
	ErrorHandlingMode string

	// Configuration source: "annotations" or "labels"
	ConfigSource string

	// Features configuration
	Features FeaturesConfig

	// Tracking
	AddTrackingAnnotations bool
	WebhookVersion         string
}

// FeaturesConfig holds feature-specific configuration
type FeaturesConfig struct {
	NestedVirtualization NestedVirtConfig
	VBiosInjection       VBiosConfig
	PCIPassthrough       PCIPassthroughConfig
	GPUDevicePlugin      GPUDevicePluginConfig
}

// NestedVirtConfig holds nested virtualization configuration
type NestedVirtConfig struct {
	Enabled       bool
	AutoDetectCPU bool
}

// VBiosConfig holds vBIOS injection configuration
type VBiosConfig struct {
	Enabled                   bool
	SidecarImage              string
	SidecarImageOverride      string
	SidecarVersion            string
	SourceConfigMapKey        string
	HookConfigMapNameTemplate string
	VBiosPath                 string
	ValidateSidecarTools      bool
	RequiredTools             []string
}

// PCIPassthroughConfig holds PCI passthrough configuration
type PCIPassthroughConfig struct {
	Enabled       bool
	ErrorHandling string
	MaxDevices    int
}

// GPUDevicePluginConfig holds GPU device plugin configuration
type GPUDevicePluginConfig struct {
	Enabled        bool
	AllowedPlugins []string
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	return &Config{
		Port:                   getEnvAsInt("PORT", 8443),
		CertDir:                getEnv("CERT_DIR", "/etc/webhook/certs"),
		LogLevel:               getEnv("LOG_LEVEL", "info"),
		ErrorHandlingMode:      getEnv("ERROR_HANDLING_MODE", utils.ErrorHandlingReject),
		ConfigSource:           getEnv("CONFIG_SOURCE", utils.ConfigSourceAnnotations),
		AddTrackingAnnotations: getEnvAsBool("ADD_TRACKING_ANNOTATIONS", true),
		WebhookVersion:         getEnv("WEBHOOK_VERSION", "v0.1.0"),
		Features: FeaturesConfig{
			NestedVirtualization: NestedVirtConfig{
				Enabled:       getEnvAsBool("FEATURE_NESTED_VIRT_ENABLED", true),
				AutoDetectCPU: getEnvAsBool("FEATURE_NESTED_VIRT_AUTO_DETECT", true),
			},
			VBiosInjection: VBiosConfig{
				Enabled:                   getEnvAsBool("FEATURE_VBIOS_ENABLED", true),
				SidecarImage:              getEnv("VBIOS_SIDECAR_IMAGE", ""),
				SidecarImageOverride:      getEnv("VBIOS_SIDECAR_IMAGE_OVERRIDE", utils.DefaultSidecarImage),
				SidecarVersion:            getEnv("VBIOS_SIDECAR_VERSION", utils.SidecarHookVersion),
				SourceConfigMapKey:        getEnv("VBIOS_SOURCE_CM_KEY", utils.VBiosConfigMapKey),
				HookConfigMapNameTemplate: getEnv("VBIOS_HOOK_CM_TEMPLATE", "{{ .VMName }}-vbios-hook"),
				VBiosPath:                 getEnv("VBIOS_PATH", "/tmp/vbios.rom"),
				ValidateSidecarTools:      getEnvAsBool("VBIOS_VALIDATE_TOOLS", true),
				RequiredTools:             getEnvAsSlice("VBIOS_REQUIRED_TOOLS", []string{"xmlstarlet", "base64"}),
			},
			PCIPassthrough: PCIPassthroughConfig{
				Enabled:       getEnvAsBool("FEATURE_PCI_PASSTHROUGH_ENABLED", true),
				ErrorHandling: getEnv("PCI_PASSTHROUGH_ERROR_HANDLING", utils.ErrorHandlingReject),
				MaxDevices:    getEnvAsInt("PCI_MAX_DEVICES", 8),
			},
			GPUDevicePlugin: GPUDevicePluginConfig{
				Enabled: getEnvAsBool("FEATURE_GPU_DEVICE_PLUGIN_ENABLED", true),
				AllowedPlugins: getEnvAsSlice("GPU_ALLOWED_PLUGINS", []string{
					"kubevirt.io/integrated-gpu",
					"nvidia.com/gpu",
				}),
			},
		},
	}
}

// Helper functions for environment variables
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := getEnv(key, "")
	if value, err := strconv.Atoi(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := getEnv(key, "")
	if value, err := strconv.ParseBool(valueStr); err == nil {
		return value
	}
	return defaultValue
}

func getEnvAsSlice(key string, defaultValue []string) []string {
	valueStr := getEnv(key, "")
	if valueStr == "" {
		return defaultValue
	}
	return strings.Split(valueStr, ",")
}
