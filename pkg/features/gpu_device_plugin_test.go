package features_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("GpuDevicePlugin", func() {
	var (
		feature *features.GpuDevicePlugin
		vm      *kubevirtv1.VirtualMachine
		ctx     context.Context
	)

	BeforeEach(func() {
		feature = features.NewGpuDevicePlugin()
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
			Expect(feature.Name()).To(Equal(utils.FeatureGpuDevicePlugin))
		})
	})

	Describe("IsEnabled", func() {
		Context("when annotation is not present", func() {
			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})

		Context("when annotation is present with device plugin name", func() {
			It("should return true", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				Expect(feature.IsEnabled(vm)).To(BeTrue())
			})
		})

		Context("when annotation is present with empty value", func() {
			It("should return false", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "",
				}
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})
	})

	Describe("Validate", func() {
		Context("when annotation is not present", func() {
			It("should return nil", func() {
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(Succeed())
			})
		})

		Context("with valid device plugin names", func() {
			It("should accept nvidia.com/gpu", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})

			It("should accept amd.com/gpu", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "amd.com/gpu",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})

			It("should accept intel.com/gpu", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "intel.com/gpu",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})

			It("should accept custom domain format", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "example.io/custom-gpu",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})
		})

		Context("with invalid device plugin names", func() {
			It("should reject names with spaces", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "invalid name",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid device plugin name"))
			})

			It("should reject names without domain", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "justgpu",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid device plugin name"))
			})

			It("should reject names with invalid characters", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu!@#",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid device plugin name"))
			})

			It("should reject empty device plugin name", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("empty"))
			})
		})
	})

	Describe("Apply", func() {
		Context("when VM template is nil", func() {
			It("should return error", func() {
				vm.Spec.Template = nil
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("template is nil"))
				Expect(result.Applied).To(BeFalse())
			})
		})

		Context("with valid device plugin", func() {
			It("should add GPU resource limit to VM spec", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Check resource limit was added
				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				Expect(limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
				Expect(limits[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(resource.MustParse("1")))
			})

			It("should add tracking annotation", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Annotations).To(HaveKey(utils.AnnotationGpuDevicePluginApplied))
				Expect(result.Annotations[utils.AnnotationGpuDevicePluginApplied]).To(Equal("nvidia.com/gpu"))
			})

			It("should work with AMD GPU", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "amd.com/gpu",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				Expect(limits).To(HaveKey(corev1.ResourceName("amd.com/gpu")))
			})

			It("should work with Intel GPU", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "intel.com/gpu",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				Expect(limits).To(HaveKey(corev1.ResourceName("intel.com/gpu")))
			})
		})

		Context("when GPU resource already exists", func() {
			It("should not override existing resource", func() {
				// Pre-populate with a different GPU resource
				vm.Spec.Template.Spec.Domain.Resources.Limits = corev1.ResourceList{
					"amd.com/gpu": resource.MustParse("2"),
				}
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				// Both should exist
				Expect(limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
				Expect(limits).To(HaveKey(corev1.ResourceName("amd.com/gpu")))
			})

			It("should skip if same GPU resource already exists", func() {
				vm.Spec.Template.Spec.Domain.Resources.Limits = corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				}
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				limits := vm.Spec.Template.Spec.Domain.Resources.Limits
				Expect(limits).To(HaveKey(corev1.ResourceName("nvidia.com/gpu")))
				Expect(limits[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(resource.MustParse("1")))
			})
		})

		Context("with invalid device plugin name", func() {
			It("should return error", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "invalid name",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid device plugin name"))
				Expect(result.Applied).To(BeFalse())
			})
		})
	})
})
