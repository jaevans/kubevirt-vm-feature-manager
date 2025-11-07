package features_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("PciPassthrough", func() {
	var (
		feature *features.PciPassthrough
		vm      *kubevirtv1.VirtualMachine
		ctx     context.Context
	)

	BeforeEach(func() {
		feature = features.NewPciPassthrough()
		ctx = context.Background()

		vm = &kubevirtv1.VirtualMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vm",
				Namespace: "default",
			},
			Spec: kubevirtv1.VirtualMachineSpec{
				Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
					Spec: kubevirtv1.VirtualMachineInstanceSpec{
						Domain: kubevirtv1.DomainSpec{},
					},
				},
			},
		}
	})

	Describe("Name", func() {
		It("should return the correct feature name", func() {
			Expect(feature.Name()).To(Equal(utils.FeaturePciPassthrough))
		})
	})

	Describe("IsEnabled", func() {
		Context("when annotation is not present", func() {
			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})

		Context("when annotation is present with valid JSON", func() {
			It("should return true", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0"]}`,
				}
				Expect(feature.IsEnabled(vm)).To(BeTrue())
			})
		})

		Context("when annotation is present with empty value", func() {
			It("should return false", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: "",
				}
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})
	})

	Describe("Validate", func() {
		Context("when annotation is not present", func() {
			It("should return nil", func() {
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})
		})

		Context("with valid PCI addresses", func() {
			It("should accept single device", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0"]}`,
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})

			It("should accept multiple devices", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0", "0000:01:00.0", "0000:03:00.1"]}`,
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})
		})

		Context("with invalid JSON", func() {
			It("should return error for malformed JSON", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{invalid json}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid JSON"))
			})
		})

		Context("with invalid PCI address format", func() {
			It("should reject address without domain", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["00:02.0"]}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid PCI address"))
			})

			It("should reject address with invalid format", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["not-a-pci-address"]}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid PCI address"))
			})

			It("should reject address with wrong separator", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000-00-02.0"]}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid PCI address"))
			})
		})

		Context("with empty devices array", func() {
			It("should return error", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": []}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("no devices specified"))
			})
		})

		Context("with duplicate devices", func() {
			It("should return error for duplicate PCI addresses", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0", "0000:00:02.0"]}`,
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("duplicate"))
			})
		})
	})

	Describe("Apply", func() {
		Context("when VM template is nil", func() {
			It("should return error", func() {
				vm.Spec.Template = nil
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0"]}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("template is nil"))
				Expect(result.Applied).To(BeFalse())
			})
		})

		Context("with valid single device", func() {
			It("should add hostDevice to VM spec", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0"]}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				devices := vm.Spec.Template.Spec.Domain.Devices.HostDevices
				Expect(devices).To(HaveLen(1))
				Expect(devices[0].Name).To(Equal("pci-device-0"))
				Expect(devices[0].DeviceName).To(Equal("pci_0000_00_02_0"))
			})

			It("should add tracking annotation", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0"]}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Annotations).To(HaveKey(utils.AnnotationPciPassthroughApplied))
				Expect(result.Annotations[utils.AnnotationPciPassthroughApplied]).To(Equal(`["0000:00:02.0"]`))
			})
		})

		Context("with multiple devices", func() {
			It("should add all devices to VM spec", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0", "0000:01:00.0", "0000:03:00.1"]}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				devices := vm.Spec.Template.Spec.Domain.Devices.HostDevices
				Expect(devices).To(HaveLen(3))
				Expect(devices[0].DeviceName).To(Equal("pci_0000_00_02_0"))
				Expect(devices[1].DeviceName).To(Equal("pci_0000_01_00_0"))
				Expect(devices[2].DeviceName).To(Equal("pci_0000_03_00_1"))
			})
		})

		Context("when devices already exist", func() {
			It("should not add duplicates", func() {
				vm.Spec.Template.Spec.Domain.Devices.HostDevices = []kubevirtv1.HostDevice{
					{
						Name:       "existing-device",
						DeviceName: "pci_0000_00_02_0",
					},
				}
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{"devices": ["0000:00:02.0", "0000:01:00.0"]}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				devices := vm.Spec.Template.Spec.Domain.Devices.HostDevices
				Expect(devices).To(HaveLen(2)) // existing + new one
				Expect(devices[1].DeviceName).To(Equal("pci_0000_01_00_0"))
			})
		})

		Context("with invalid JSON in annotation", func() {
			It("should return error", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationPciPassthrough: `{invalid}`,
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid JSON"))
				Expect(result.Applied).To(BeFalse())
			})
		})
	})
})
