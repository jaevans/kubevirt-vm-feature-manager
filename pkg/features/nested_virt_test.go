package features_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("NestedVirtualization", func() {
	var (
		feature *features.NestedVirtualization
		vm      *kubevirtv1.VirtualMachine
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create feature with default config
		cfg := &config.NestedVirtConfig{
			Enabled:       true,
			AutoDetectCPU: true,
		}
		feature = features.NewNestedVirtualization(cfg)

		// Create basic VM
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
			Expect(feature.Name()).To(Equal(utils.FeatureNestedVirt))
		})
	})

	Describe("IsEnabled", func() {
		Context("when annotation is set to enabled", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
			})

			It("should return true", func() {
				Expect(feature.IsEnabled(vm)).To(BeTrue())
			})
		})

		Context("when annotation is not set", func() {
			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})

		Context("when annotation has wrong value", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "disabled",
				}
			})

			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})

		Context("when feature is disabled in config", func() {
			BeforeEach(func() {
				cfg := &config.NestedVirtConfig{
					Enabled:       false,
					AutoDetectCPU: true,
				}
				feature = features.NewNestedVirtualization(cfg)
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
			})

			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})
	})

	Describe("Validate", func() {
		Context("when annotation value is valid", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
			})

			It("should not return error", func() {
				err := feature.Validate(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})

		Context("when annotation value is invalid", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "invalid-value",
				}
			})

			It("should return error", func() {
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid value"))
			})
		})

		Context("when feature is not enabled", func() {
			It("should not return error", func() {
				err := feature.Validate(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})

	Describe("Apply", func() {
		Context("when feature is not enabled", func() {
			It("should not modify VM and return empty result", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeFalse())
			})
		})

		Context("when feature is enabled", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
			})

			It("should add CPU feature to VM spec", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Check CPU feature was added
				Expect(vm.Spec.Template.Spec.Domain.CPU).ToNot(BeNil())
				Expect(vm.Spec.Template.Spec.Domain.CPU.Features).ToNot(BeEmpty())

				cpuFeature := vm.Spec.Template.Spec.Domain.CPU.Features[0]
				// Should be either AMD SVM or Intel VMX
				Expect(cpuFeature.Name).To(Or(
					Equal(utils.CPUFeatureSVM),
					Equal(utils.CPUFeatureVMX),
				))
				Expect(cpuFeature.Policy).To(Equal("require"))
			})

			It("should return mutation result with annotations", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				Expect(result.Annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
				Expect(result.Annotations[utils.AnnotationNestedVirtApplied]).To(Equal("true"))
			})

			It("should return informational messages", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Messages).ToNot(BeEmpty())
				Expect(result.Messages[0]).To(ContainSubstring("nested virtualization"))
			})
		})

		Context("when CPU feature already exists", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
				// Pre-populate CPU with SVM feature
				vm.Spec.Template.Spec.Domain.CPU = &kubevirtv1.CPU{
					Features: []kubevirtv1.CPUFeature{
						{
							Name:   utils.CPUFeatureSVM,
							Policy: "require",
						},
					},
				}
			})

			It("should not duplicate the CPU feature", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Should still only have one feature
				Expect(vm.Spec.Template.Spec.Domain.CPU.Features).To(HaveLen(1))
			})
		})

		Context("when VM template is nil", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
				vm.Spec.Template = nil
			})

			It("should initialize template and add CPU feature", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				Expect(vm.Spec.Template).ToNot(BeNil())
				Expect(vm.Spec.Template.Spec.Domain.CPU).ToNot(BeNil())
				Expect(vm.Spec.Template.Spec.Domain.CPU.Features).ToNot(BeEmpty())
			})
		})

		Context("when VM has existing CPU features", func() {
			BeforeEach(func() {
				vm.Annotations = map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				}
				// Add different CPU feature
				vm.Spec.Template.Spec.Domain.CPU = &kubevirtv1.CPU{
					Features: []kubevirtv1.CPUFeature{
						{
							Name:   "some-other-feature",
							Policy: "require",
						},
					},
				}
			})

			It("should preserve existing CPU features", func() {
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Should have both features
				Expect(vm.Spec.Template.Spec.Domain.CPU.Features).To(HaveLen(2))

				featureNames := []string{
					vm.Spec.Template.Spec.Domain.CPU.Features[0].Name,
					vm.Spec.Template.Spec.Domain.CPU.Features[1].Name,
				}
				Expect(featureNames).To(ContainElement("some-other-feature"))
			})
		})
	})
})
