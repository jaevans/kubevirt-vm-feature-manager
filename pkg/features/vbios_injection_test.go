package features_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("VBiosInjection", func() {
	var (
		feature *features.VBiosInjection
		vm      *kubevirtv1.VirtualMachine
		ctx     context.Context
	)

	BeforeEach(func() {
		feature = features.NewVBiosInjection()
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
			Expect(feature.Name()).To(Equal(utils.FeatureVBiosInjection))
		})
	})

	Describe("IsEnabled", func() {
		Context("when annotation is not present", func() {
			It("should return false", func() {
				Expect(feature.IsEnabled(vm)).To(BeFalse())
			})
		})

		Context("when annotation is present with ConfigMap name", func() {
			It("should return true", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				Expect(feature.IsEnabled(vm)).To(BeTrue())
			})
		})

		Context("when annotation is present with empty value", func() {
			It("should return false", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "",
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

		Context("with valid ConfigMap name", func() {
			It("should accept valid ConfigMap name", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})
		})

		Context("with invalid ConfigMap name", func() {
			It("should reject names with invalid characters", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "invalid_name!",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid ConfigMap name"))
			})

			It("should reject empty ConfigMap name", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("empty"))
			})

			It("should reject ConfigMap name that is too long", func() {
				longName := make([]byte, 254)
				for i := range longName {
					longName[i] = 'a'
				}
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: string(longName),
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("too long"))
			})
		})

		Context("with sidecar image override", func() {
			It("should accept valid image reference", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios",
					utils.AnnotationSidecarImage:   "registry.example.com/kubevirt/sidecar:v1.4.0",
				}
				Expect(feature.Validate(ctx, vm, nil)).To(Succeed())
			})

			It("should reject invalid image reference", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios",
					utils.AnnotationSidecarImage:   "invalid image name with spaces",
				}
				err := feature.Validate(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid sidecar image"))
			})
		})
	})

	Describe("Apply", func() {
		Context("when VM template is nil", func() {
			It("should return error", func() {
				vm.Spec.Template = nil
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("template is nil"))
				Expect(result.Applied).To(BeFalse())
			})
		})

		Context("with valid vBIOS ConfigMap", func() {
			It("should add hook sidecar to VM template", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Check hook annotation was added
				Expect(vm.Spec.Template.ObjectMeta.Annotations).To(HaveKey(utils.HookAnnotationKey))
			})

			It("should add vBIOS volume to VM spec", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				// Check volume was added
				volumes := vm.Spec.Template.Spec.Volumes
				Expect(volumes).To(HaveLen(1))
				Expect(volumes[0].Name).To(Equal("vbios-rom"))
				Expect(volumes[0].ConfigMap).ToNot(BeNil())
				Expect(volumes[0].ConfigMap.Name).To(Equal("my-vbios-configmap"))
			})

			It("should add tracking annotation", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Annotations).To(HaveKey(utils.AnnotationVBiosInjectionApplied))
				Expect(result.Annotations[utils.AnnotationVBiosInjectionApplied]).To(Equal("my-vbios-configmap"))
			})

			It("should use default sidecar image", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				hookAnnotation := vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey]
				Expect(hookAnnotation).To(ContainSubstring(utils.DefaultSidecarImage))
			})

			It("should use custom sidecar image when provided", func() {
				customImage := "registry.example.com/custom-sidecar:v1.5.0"
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
					utils.AnnotationSidecarImage:   customImage,
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				hookAnnotation := vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey]
				Expect(hookAnnotation).To(ContainSubstring(customImage))
			})

			It("should configure sidecar with correct hook type", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				_, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())

				hookAnnotation := vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey]
				Expect(hookAnnotation).To(ContainSubstring(utils.SidecarHookType))
				Expect(hookAnnotation).To(ContainSubstring(utils.SidecarHookVersion))
			})
		})

		Context("when vBIOS volume already exists", func() {
			It("should not add duplicate volume", func() {
				vm.Spec.Template.Spec.Volumes = []kubevirtv1.Volume{
					{
						Name: "vbios-rom",
						VolumeSource: kubevirtv1.VolumeSource{
							ConfigMap: &kubevirtv1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "existing-vbios",
								},
							},
						},
					},
				}
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Should still only have one volume
				Expect(vm.Spec.Template.Spec.Volumes).To(HaveLen(1))
			})
		})

		Context("when hook sidecar already exists", func() {
			It("should not add duplicate hook sidecar", func() {
				existingHook := `[{"image":"registry.k8s.io/kubevirt/sidecar-shim:v1.3.0"}]`
				if vm.Spec.Template.ObjectMeta.Annotations == nil {
					vm.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
				}
				vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey] = existingHook

				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "my-vbios-configmap",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Applied).To(BeTrue())

				// Hook should still be present (not removed)
				Expect(vm.Spec.Template.ObjectMeta.Annotations).To(HaveKey(utils.HookAnnotationKey))
			})
		})

		Context("with invalid ConfigMap name", func() {
			It("should return error", func() {
				vm.Annotations = map[string]string{
					utils.AnnotationVBiosInjection: "invalid name!",
				}
				result, err := feature.Apply(ctx, vm, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid ConfigMap name"))
				Expect(result.Applied).To(BeFalse())
			})
		})
	})
})
