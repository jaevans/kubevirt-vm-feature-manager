package config_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("Config", func() {
	var originalEnv map[string]string

	BeforeEach(func() {
		// Save original environment
		originalEnv = make(map[string]string)
		envVars := []string{
			"PORT", "CERT_DIR", "LOG_LEVEL", "ERROR_HANDLING_MODE",
			"ADD_TRACKING_ANNOTATIONS", "WEBHOOK_VERSION",
			"FEATURE_NESTED_VIRT_ENABLED", "FEATURE_NESTED_VIRT_AUTO_DETECT",
			"FEATURE_VBIOS_ENABLED", "VBIOS_SIDECAR_IMAGE",
		}
		for _, key := range envVars {
			originalEnv[key] = os.Getenv(key)
			Expect(os.Unsetenv(key)).To(Succeed())
		}
	})

	AfterEach(func() {
		// Restore original environment
		for key, value := range originalEnv {
			if value == "" {
				Expect(os.Unsetenv(key)).To(Succeed())
			} else {
				Expect(os.Setenv(key, value)).To(Succeed())
			}
		}
	})

	Describe("LoadConfig", func() {
		Context("with default values", func() {
			It("should load default configuration", func() {
				cfg := config.LoadConfig()

				Expect(cfg.Port).To(Equal(8443))
				Expect(cfg.CertDir).To(Equal("/etc/webhook/certs"))
				Expect(cfg.LogLevel).To(Equal("info"))
				Expect(cfg.ErrorHandlingMode).To(Equal(utils.ErrorHandlingReject))
				Expect(cfg.AddTrackingAnnotations).To(BeTrue())
				Expect(cfg.WebhookVersion).To(Equal("v0.1.0"))
			})

			It("should enable all features by default", func() {
				cfg := config.LoadConfig()

				Expect(cfg.Features.NestedVirtualization.Enabled).To(BeTrue())
				Expect(cfg.Features.NestedVirtualization.AutoDetectCPU).To(BeTrue())
				Expect(cfg.Features.VBiosInjection.Enabled).To(BeTrue())
				Expect(cfg.Features.PCIPassthrough.Enabled).To(BeTrue())
				Expect(cfg.Features.GPUDevicePlugin.Enabled).To(BeTrue())
			})

			It("should set vBIOS defaults correctly", func() {
				cfg := config.LoadConfig()

				Expect(cfg.Features.VBiosInjection.SidecarImageOverride).To(Equal(utils.DefaultSidecarImage))
				Expect(cfg.Features.VBiosInjection.SidecarVersion).To(Equal(utils.SidecarHookVersion))
				Expect(cfg.Features.VBiosInjection.SourceConfigMapKey).To(Equal(utils.VBiosConfigMapKey))
				Expect(cfg.Features.VBiosInjection.VBiosPath).To(Equal("/tmp/vbios.rom"))
				Expect(cfg.Features.VBiosInjection.ValidateSidecarTools).To(BeTrue())
			})
		})

		Context("with custom environment variables", func() {
			It("should override port from environment", func() {
				Expect(os.Setenv("PORT", "9443")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.Port).To(Equal(9443))
			})

			It("should override log level from environment", func() {
				Expect(os.Setenv("LOG_LEVEL", "debug")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.LogLevel).To(Equal("debug"))
			})

			It("should override error handling mode from environment", func() {
				Expect(os.Setenv("ERROR_HANDLING_MODE", utils.ErrorHandlingAllowAndLog)).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.ErrorHandlingMode).To(Equal(utils.ErrorHandlingAllowAndLog))
			})

			It("should disable tracking annotations from environment", func() {
				Expect(os.Setenv("ADD_TRACKING_ANNOTATIONS", "false")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.AddTrackingAnnotations).To(BeFalse())
			})

			It("should disable features from environment", func() {
				Expect(os.Setenv("FEATURE_NESTED_VIRT_ENABLED", "false")).To(Succeed())
				Expect(os.Setenv("FEATURE_VBIOS_ENABLED", "false")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.Features.NestedVirtualization.Enabled).To(BeFalse())
				Expect(cfg.Features.VBiosInjection.Enabled).To(BeFalse())
			})

			It("should override vBIOS sidecar image from environment", func() {
				customImage := "myregistry.io/sidecar-shim:custom"
				Expect(os.Setenv("VBIOS_SIDECAR_IMAGE_OVERRIDE", customImage)).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.Features.VBiosInjection.SidecarImageOverride).To(Equal(customImage))
			})

			It("should parse GPU allowed plugins from environment", func() {
				Expect(os.Setenv("GPU_ALLOWED_PLUGINS", "plugin1,plugin2,plugin3")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.Features.GPUDevicePlugin.AllowedPlugins).To(ConsistOf("plugin1", "plugin2", "plugin3"))
			})
		})

		Context("with invalid environment values", func() {
			It("should use default for invalid port", func() {
				Expect(os.Setenv("PORT", "invalid")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.Port).To(Equal(8443))
			})

			It("should use default for invalid boolean", func() {
				Expect(os.Setenv("ADD_TRACKING_ANNOTATIONS", "not-a-bool")).To(Succeed())
				cfg := config.LoadConfig()
				Expect(cfg.AddTrackingAnnotations).To(BeTrue())
			})
		})
	})
})
