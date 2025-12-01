package integration_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("Integration Tests", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
		cfg        *config.Config
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithCancel(ctx)

		// Create test config
		cfg = &config.Config{
			Features: config.FeaturesConfig{
				NestedVirtualization: config.NestedVirtConfig{
					Enabled: true,
				},
				PCIPassthrough: config.PCIPassthroughConfig{
					Enabled:    true,
					MaxDevices: 8,
				},
				VBiosInjection: config.VBiosConfig{
					Enabled: true,
				},
				GPUDevicePlugin: config.GPUDevicePluginConfig{
					Enabled: true,
				},
			},
		}
	})

	AfterEach(func() {
		testCancel()
	})

	Describe("Nested Virtualization", func() {
		It("should add CPU features to VM", func() {
			vm := createBasicVM("nested-virt-vm", "integration-test", map[string]string{
				utils.AnnotationNestedVirt: "enabled",
			})

			// Apply mutations directly (not through admission webhook)
			feature := features.NewNestedVirtualization(&cfg.Features.NestedVirtualization, utils.ConfigSourceAnnotations)
			Expect(feature.IsEnabled(vm)).To(BeTrue())

			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			result, err := feature.Apply(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Applied).To(BeTrue())

			// Apply tracking annotations
			if vm.Annotations == nil {
				vm.Annotations = make(map[string]string)
			}
			for k, v := range result.Annotations {
				vm.Annotations[k] = v
			}

			// Verify CPU features were added
			Expect(vm.Spec.Template.Spec.Domain.CPU).NotTo(BeNil())
			Expect(vm.Spec.Template.Spec.Domain.CPU.Features).NotTo(BeEmpty())

			// Should have either svm or vmx
			cpuFeature := vm.Spec.Template.Spec.Domain.CPU.Features[0]
			Expect(cpuFeature.Name).To(Or(Equal(utils.CPUFeatureSVM), Equal(utils.CPUFeatureVMX)))
			Expect(cpuFeature.Policy).To(Equal("require"))

			// Verify tracking annotation
			Expect(vm.Annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
		})
	})

	Describe("PCI Passthrough", func() {
		It("should add PCI devices to VM", func() {
			vm := createBasicVM("pci-vm", "integration-test", map[string]string{
				utils.AnnotationPciPassthrough: `{"devices":["0000:00:14.0"]}`,
			})

			// Apply mutations
			feature := features.NewPciPassthrough(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			result, err := feature.Apply(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Applied).To(BeTrue())

			// Verify PCI device was added
			Expect(vm.Spec.Template.Spec.Domain.Devices.HostDevices).To(HaveLen(1))
			hostDev := vm.Spec.Template.Spec.Domain.Devices.HostDevices[0]
			Expect(hostDev.Name).To(Equal("pci-device-0"))
			Expect(hostDev.DeviceName).To(Equal("pci_0000_00_14_0"))
		})

		It("should handle multiple PCI devices", func() {
			vm := createBasicVM("multi-pci-vm", "integration-test", map[string]string{
				utils.AnnotationPciPassthrough: `{"devices":["0000:00:14.0","0000:03:00.0"]}`,
			})

			feature := features.NewPciPassthrough(utils.ConfigSourceAnnotations)
			_, err := feature.Apply(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			Expect(vm.Spec.Template.Spec.Domain.Devices.HostDevices).To(HaveLen(2))
		})
	})

	Describe("vBIOS Injection", func() {
		var configMap *corev1.ConfigMap

		BeforeEach(func() {
			// Create ConfigMap with vBIOS data
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vbios",
					Namespace: "integration-test",
				},
				BinaryData: map[string][]byte{
					utils.VBiosConfigMapKey: []byte("fake-vbios-data"),
				},
			}
			err := k8sClient.Create(testCtx, configMap)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if configMap != nil {
				_ = k8sClient.Delete(testCtx, configMap)
			}
		})

		It("should inject vBIOS ConfigMap and hook sidecar", func() {
			vm := createBasicVM("vbios-vm", "integration-test", map[string]string{
				utils.AnnotationVBiosInjection: "test-vbios",
			})

			feature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			result, err := feature.Apply(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Applied).To(BeTrue())

			// Verify volume was added
			foundVolume := false
			for _, vol := range vm.Spec.Template.Spec.Volumes {
				if vol.Name == "vbios-rom" {
					foundVolume = true
					Expect(vol.ConfigMap).NotTo(BeNil())
					Expect(vol.ConfigMap.Name).To(Equal("test-vbios"))
				}
			}
			Expect(foundVolume).To(BeTrue())

			// Verify hook sidecar annotation was added
			Expect(vm.Spec.Template.ObjectMeta.Annotations).To(HaveKey(utils.HookAnnotationKey))
		})

		It("should validate ConfigMap name format", func() {
			vm := createBasicVM("vbios-invalid-name-vm", "integration-test", map[string]string{
				utils.AnnotationVBiosInjection: "Invalid_Name_With_Underscores!",
			})

			feature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid ConfigMap name"))
		})
	})

	Describe("GPU Device Plugin", func() {
		It("should add GPU resource limit", func() {
			vm := createBasicVM("gpu-vm", "integration-test", map[string]string{
				utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
			})

			feature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())

			result, err := feature.Apply(testCtx, vm, k8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Applied).To(BeTrue())

			// Verify resource limit was added
			limits := vm.Spec.Template.Spec.Domain.Resources.Limits
			Expect(limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
			Expect(limits[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(resource.MustParse("1")))
		})

		It("should support different GPU vendors", func() {
			vendors := []string{"nvidia.com/gpu", "amd.com/gpu", "intel.com/gpu"}

			for _, vendor := range vendors {
				vm := createBasicVM("gpu-vm-"+vendor, "integration-test", map[string]string{
					utils.AnnotationGpuDevicePlugin: vendor,
				})

				feature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				_, err := feature.Apply(testCtx, vm, k8sClient)
				Expect(err).NotTo(HaveOccurred())

				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				Expect(limits).To(HaveKey(corev1.ResourceName(vendor)))
			}
		})
	})

	Describe("Combined Features", func() {
		var configMap *corev1.ConfigMap

		BeforeEach(func() {
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "combined-vbios",
					Namespace: "integration-test",
				},
				BinaryData: map[string][]byte{
					utils.VBiosConfigMapKey: []byte("fake-vbios-data"),
				},
			}
			err := k8sClient.Create(testCtx, configMap)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			if configMap != nil {
				_ = k8sClient.Delete(testCtx, configMap)
			}
		})

		It("should apply all features together", func() {
			vm := createBasicVM("all-features-vm", "integration-test", map[string]string{
				utils.AnnotationNestedVirt:      "enabled",
				utils.AnnotationPciPassthrough:  `{"devices":["0000:00:14.0"]}`,
				utils.AnnotationVBiosInjection:  "combined-vbios",
				utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
			})

			// Apply all features
			allFeatures := []features.Feature{
				features.NewNestedVirtualization(&cfg.Features.NestedVirtualization, utils.ConfigSourceAnnotations),
				features.NewPciPassthrough(utils.ConfigSourceAnnotations),
				features.NewVBiosInjection(utils.ConfigSourceAnnotations),
				features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations),
			}

			for _, feature := range allFeatures {
				if feature.IsEnabled(vm) {
					err := feature.Validate(testCtx, vm, k8sClient)
					Expect(err).NotTo(HaveOccurred())

					result, err := feature.Apply(testCtx, vm, k8sClient)
					Expect(err).NotTo(HaveOccurred())
					Expect(result.Applied).To(BeTrue())
				}
			}

			// Verify all features were applied
			// 1. Nested virt: CPU features
			Expect(vm.Spec.Template.Spec.Domain.CPU.Features).NotTo(BeEmpty())

			// 2. PCI Passthrough: Host devices
			Expect(vm.Spec.Template.Spec.Domain.Devices.HostDevices).To(HaveLen(1))

			// 3. vBIOS: Volume and hook
			foundVolume := false
			for _, vol := range vm.Spec.Template.Spec.Volumes {
				if vol.Name == "vbios-rom" {
					foundVolume = true
				}
			}
			Expect(foundVolume).To(BeTrue())
			Expect(vm.Spec.Template.ObjectMeta.Annotations).To(HaveKey(utils.HookAnnotationKey))

			// 4. GPU: Resource limit
			limits := vm.Spec.Template.Spec.Domain.Resources.Limits
			Expect(limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
		})
	})

	Describe("Error Handling", func() {
		It("should handle validation errors appropriately", func() {
			vm := createBasicVM("invalid-pci-vm", "integration-test", map[string]string{
				utils.AnnotationPciPassthrough: `{"devices":["invalid"]}`,
			})

			feature := features.NewPciPassthrough(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid PCI address"))
		})

		It("should prevent duplicate PCI devices", func() {
			vm := createBasicVM("dup-pci-vm", "integration-test", map[string]string{
				utils.AnnotationPciPassthrough: `{"devices":["0000:00:14.0","0000:00:14.0"]}`,
			})

			feature := features.NewPciPassthrough(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("duplicate"))
		})

		It("should reject invalid GPU device plugin names", func() {
			vm := createBasicVM("invalid-gpu-vm", "integration-test", map[string]string{
				utils.AnnotationGpuDevicePlugin: "invalid name with spaces",
			})

			feature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
			err := feature.Validate(testCtx, vm, k8sClient)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid device plugin name"))
		})
	})
})
