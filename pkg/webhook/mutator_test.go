package webhook

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var _ = Describe("Mutator", func() {
	var (
		mutator *Mutator
		cfg     *config.Config
		ctx     context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = &config.Config{
			AddTrackingAnnotations: true,
			ErrorHandlingMode:      utils.ErrorHandlingReject,
			ConfigSource:           utils.ConfigSourceAnnotations,
		}
	})

	Describe("Handle", func() {
		Context("with no features enabled", func() {
			It("should allow VM without mutation", func() {
				vm := &kubevirtv1.VirtualMachine{
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

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("No features requested"))
			})
		})

		Context("with nested virtualization enabled", func() {
			It("should apply nested virt feature and add tracking annotations", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt: "enabled",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())
				Expect(response.PatchType).ToNot(BeNil())
				Expect(*response.PatchType).To(Equal(admissionv1.PatchTypeJSONPatch))

				// Verify the patch contains actual mutations
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())
				Expect(patchOps).ToNot(BeEmpty())

				// Verify spec patch contains CPU features
				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "spec patch value should be a map")

						// Navigate to CPU features
						template, ok := spec["template"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "template should exist in spec")
						specMap, ok := template["spec"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "spec should exist in template")
						domain, ok := specMap["domain"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "domain should exist in spec")
						cpu, ok := domain["cpu"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "CPU should be present")
						features, ok := cpu["features"].([]interface{})
						Expect(ok).To(BeTrue(), "CPU features should be present")
						Expect(features).ToNot(BeEmpty(), "CPU features should not be empty")

						// Verify CPU feature is svm or vmx
						cpuFeature, ok := features[0].(map[string]interface{})
						Expect(ok).To(BeTrue())
						name, ok := cpuFeature["name"].(string)
						Expect(ok).To(BeTrue())
						Expect(name).To(Or(Equal("svm"), Equal("vmx")))
						policy, ok := cpuFeature["policy"].(string)
						Expect(ok).To(BeTrue())
						Expect(policy).To(Equal("require"))
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue(), "should have a spec patch operation")

				// Verify annotations patch contains tracking annotation
				foundAnnotationsPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						foundAnnotationsPatch = true
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "annotations patch value should be a map")
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						Expect(annotations[utils.AnnotationNestedVirtApplied]).To(Equal("true"))
						break
					}
				}
				Expect(foundAnnotationsPatch).To(BeTrue(), "should have an annotations patch operation")
			})

			It("should not add tracking annotations when disabled in config", func() {
				cfg.AddTrackingAnnotations = false

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt: "enabled",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())

				// Verify patch does NOT contain tracking annotations
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Check that annotations patch either doesn't exist or doesn't contain tracking
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())
						// Should only have the original nested-virt annotation, not the "applied" tracking one
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirt))
						Expect(annotations).ToNot(HaveKey(utils.AnnotationNestedVirtApplied))
					}
				}
			})
		})

		Context("with multiple features enabled", func() {
			It("should apply all enabled features", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt:      "enabled",
							utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature, gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify both features are applied in the patch
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Verify spec patch contains both CPU features and GPU resource limits
				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())

						// Check CPU features (nested virt)
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})
						cpu := domain["cpu"].(map[string]interface{})
						cpuFeatures := cpu["features"].([]interface{})
						Expect(cpuFeatures).ToNot(BeEmpty(), "CPU features should be present")

						// Check GPU resource limits
						resources := domain["resources"].(map[string]interface{})
						limits := resources["limits"].(map[string]interface{})
						Expect(limits).To(HaveKey("nvidia.com/gpu"), "GPU resource limit should be present")
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())

				// Verify tracking annotations for both features
				foundAnnotationsPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						foundAnnotationsPatch = true
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						Expect(annotations).To(HaveKey(utils.AnnotationGpuDevicePluginApplied))
						break
					}
				}
				Expect(foundAnnotationsPatch).To(BeTrue())
			})
		})

		Context("with validation error", func() {
			It("should reject VM with invalid GPU device plugin name", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationGpuDevicePlugin: "invalid name with spaces",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
			})
		})

		Context("with application error", func() {
			It("should reject VM when feature application fails", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationVBiosInjection: "test-vbios",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: nil, // This will cause an error
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(ContainSubstring("template is nil"))
			})
		})

		Context("with invalid VM JSON", func() {
			It("should return error response", func() {
				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte("invalid json"),
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
			})
		})
	})

	Describe("Error Handling Modes", func() {
		Context("with ErrorHandlingReject mode", func() {
			It("should reject VM on feature error", func() {
				cfg.ErrorHandlingMode = utils.ErrorHandlingReject

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationVBiosInjection: "test-vbios",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: nil,
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.Allowed).To(BeFalse())
			})
		})

		Context("with ErrorHandlingAllowAndLog mode", func() {
			It("should allow VM on feature error", func() {
				cfg.ErrorHandlingMode = utils.ErrorHandlingAllowAndLog

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationVBiosInjection: "test-vbios",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: nil,
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("allowed"))
			})
		})

		Context("with ErrorHandlingStripLabel mode", func() {
			It("should allow VM on feature error and strip the annotation", func() {
				cfg.ErrorHandlingMode = utils.ErrorHandlingStripLabel

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationVBiosInjection: "test-vbios",
							"other-annotation":             "should-remain",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: nil,
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("annotation"))
				Expect(response.Result.Message).To(ContainSubstring("stripped"))

				// Verify the patch actually strips the annotation
				Expect(response.Patch).ToNot(BeNil())
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Find the annotations patch operation
				foundAnnotationPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						foundAnnotationPatch = true
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "annotations patch value should be a map")
						// The failing annotation should be stripped
						Expect(annotations).ToNot(HaveKey(utils.AnnotationVBiosInjection))
						// Other annotations should remain
						Expect(annotations).To(HaveKey("other-annotation"))
						break
					}
				}
				Expect(foundAnnotationPatch).To(BeTrue(), "should have an annotations patch operation")
			})
		})
	})

	Describe("createPatch", func() {
		It("should create valid JSON patch", func() {
			original := &kubevirtv1.VirtualMachine{
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

			mutated := original.DeepCopy()
			mutated.Annotations = map[string]string{
				"test-key": "test-value",
			}
			mutated.Spec.Template.Spec.Domain.CPU = &kubevirtv1.CPU{
				Features: []kubevirtv1.CPUFeature{
					{Name: "svm", Policy: "require"},
				},
			}

			mutator = NewMutator(nil, cfg, []features.Feature{})
			patch, err := mutator.createPatch(original, mutated)

			Expect(err).ToNot(HaveOccurred())
			Expect(patch).ToNot(BeNil())

			// Verify it's valid JSON
			var patchOps []map[string]interface{}
			err = json.Unmarshal(patch, &patchOps)
			Expect(err).ToNot(HaveOccurred())
			Expect(patchOps).ToNot(BeEmpty())
		})
	})

	Describe("hasEnabledFeatures", func() {
		It("should return true when features are enabled", func() {
			vm := &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.AnnotationNestedVirt: "enabled",
					},
				},
			}

			nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
				Enabled:       true,
				AutoDetectCPU: true,
			}, utils.ConfigSourceAnnotations)
			mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

			Expect(mutator.hasEnabledFeatures(vm)).To(BeTrue())
		})

		It("should return false when no features are enabled", func() {
			vm := &kubevirtv1.VirtualMachine{
				ObjectMeta: metav1.ObjectMeta{},
			}

			nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
				Enabled:       true,
				AutoDetectCPU: true,
			}, utils.ConfigSourceAnnotations)
			mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

			Expect(mutator.hasEnabledFeatures(vm)).To(BeFalse())
		})
	})

	Describe("Edge Cases and Additional Coverage", func() {
		Context("with unknown error handling mode", func() {
			It("should use default error handling", func() {
				cfg.ErrorHandlingMode = "unknown-mode"

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationVBiosInjection: "test-vbios",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: nil,
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response.Allowed).To(BeFalse())
			})
		})

		Context("with UPDATE operation", func() {
			It("should process update requests same as create", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt: "enabled",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-update",
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify the patch is applied correctly for updates
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())
				Expect(patchOps).ToNot(BeEmpty())
			})

			It("should handle adding feature annotation during update", func() {
				// Original VM without features
				oldVM := &kubevirtv1.VirtualMachine{
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

				// Updated VM with new feature annotation
				newVM := oldVM.DeepCopy()
				newVM.Annotations = map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				}

				vmBytes, err := json.Marshal(newVM)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-update-add",
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())

				// Verify GPU resource was added
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec := op["value"].(map[string]interface{})
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})
						resources := domain["resources"].(map[string]interface{})
						limits := resources["limits"].(map[string]interface{})
						Expect(limits).To(HaveKey("nvidia.com/gpu"))
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())
			})
		})
	})

	Describe("Userdata Feature Integration", func() {
		Context("with userdata feature directives and no annotations", func() {
			It("should apply features from userdata", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
# @kubevirt-feature: nested-virt=enabled
users:
  - name: ubuntu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())
				Expect(response.PatchType).ToNot(BeNil())
				Expect(*response.PatchType).To(Equal(admissionv1.PatchTypeJSONPatch))

				// Verify the patch contains CPU features from nested virt
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Verify spec patch contains CPU features
				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "spec patch value should be a map")

						template, ok := spec["template"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "template should exist in spec")
						specMap, ok := template["spec"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "spec should exist in template")
						domain, ok := specMap["domain"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "domain should exist in spec")
						cpu, ok := domain["cpu"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "CPU should be present")
						cpuFeatures, ok := cpu["features"].([]interface{})
						Expect(ok).To(BeTrue(), "CPU features should be present")
						Expect(cpuFeatures).ToNot(BeEmpty(), "CPU features should not be empty")
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue(), "should have a spec patch operation")

				// Verify annotations patch contains both the merged userdata annotation and tracking annotation
				foundAnnotationsPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						foundAnnotationsPatch = true
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "annotations patch value should be a map")
						// Should have the userdata-derived annotation merged in
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirt))
						Expect(annotations[utils.AnnotationNestedVirt]).To(Equal("enabled"))
						// Should have tracking annotation
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						break
					}
				}
				Expect(foundAnnotationsPatch).To(BeTrue(), "should have an annotations patch operation")
			})

			It("should apply multiple features from userdata", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
# @kubevirt-feature: nested-virt=enabled
# @kubevirt-feature: gpu-device-plugin=nvidia.com/gpu
users:
  - name: ubuntu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature, gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify the patch contains both CPU features and GPU resource limits
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Check annotations patch has both merged annotations
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirt))
						Expect(annotations).To(HaveKey(utils.AnnotationGpuDevicePlugin))
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						Expect(annotations).To(HaveKey(utils.AnnotationGpuDevicePluginApplied))
						break
					}
				}
			})
		})

		Context("with both userdata features and annotations", func() {
			It("should give precedence to annotations over userdata", func() {
				// VM has annotation with different value than userdata
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							// Annotation specifies a different GPU than userdata
							utils.AnnotationGpuDevicePlugin: "amd.com/gpu",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
# @kubevirt-feature: gpu-device-plugin=nvidia.com/gpu
users:
  - name: ubuntu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify the patch uses the annotation value (amd.com/gpu), not userdata value (nvidia.com/gpu)
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec := op["value"].(map[string]interface{})
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})
						resources := domain["resources"].(map[string]interface{})
						limits := resources["limits"].(map[string]interface{})
						// Annotation value should take precedence
						Expect(limits).To(HaveKey("amd.com/gpu"), "annotation value should be used")
						Expect(limits).ToNot(HaveKey("nvidia.com/gpu"), "userdata value should be overridden")
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())
			})

			It("should merge non-conflicting features from both sources", func() {
				// Annotation has nested-virt, userdata has gpu-device-plugin
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt: "enabled",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
# @kubevirt-feature: gpu-device-plugin=nvidia.com/gpu
users:
  - name: ubuntu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				gpuFeature := features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature, gpuFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify both features were applied
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec := op["value"].(map[string]interface{})
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})

						// Check CPU features from nested virt (from annotation)
						cpu := domain["cpu"].(map[string]interface{})
						cpuFeatures := cpu["features"].([]interface{})
						Expect(cpuFeatures).ToNot(BeEmpty())

						// Check GPU resource limits (from userdata)
						resources := domain["resources"].(map[string]interface{})
						limits := resources["limits"].(map[string]interface{})
						Expect(limits).To(HaveKey("nvidia.com/gpu"))
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())

				// Verify annotations contain both the original and merged annotations
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirt))
						Expect(annotations).To(HaveKey(utils.AnnotationGpuDevicePlugin))
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						Expect(annotations).To(HaveKey(utils.AnnotationGpuDevicePluginApplied))
						break
					}
				}
			})
		})

		Context("with invalid userdata", func() {
			It("should continue with annotation-based features when secret reference fails", func() {
				// VM references a secret that doesn't exist (parser will fail non-fatally)
				// The mutator should still process the annotation-based feature
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				_ = kubevirtv1.AddToScheme(scheme)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						Annotations: map[string]string{
							utils.AnnotationNestedVirt: "enabled",
						},
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												// Reference a secret that doesn't exist
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "non-existent-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(fakeClient, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify the annotation-based feature was still applied
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec := op["value"].(map[string]interface{})
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})
						cpu := domain["cpu"].(map[string]interface{})
						cpuFeatures := cpu["features"].([]interface{})
						Expect(cpuFeatures).ToNot(BeEmpty())
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())
			})

			It("should allow VM with no features when userdata parsing fails", func() {
				// VM only has secret ref that fails, no annotations - should allow without mutation
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				_ = kubevirtv1.AddToScheme(scheme)
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												// Reference a secret that doesn't exist
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "non-existent-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(fakeClient, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				// Should have no mutation (no patch) since no features are enabled
				Expect(response.Result.Message).To(ContainSubstring("No features requested"))
			})

			It("should handle userdata with features from existing secret", func() {
				// Test that userdata is successfully parsed from a secret that exists
				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				_ = kubevirtv1.AddToScheme(scheme)

				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-userdata-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"userdata": []byte(`#cloud-config
# @kubevirt-feature: nested-virt=enabled
users:
  - name: ubuntu
`),
					},
				}
				fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Domain: kubevirtv1.DomainSpec{},
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "test-userdata-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
				}, utils.ConfigSourceAnnotations)
				mutator = NewMutator(fakeClient, cfg, []features.Feature{nestedVirtFeature})

				response, err := mutator.Handle(ctx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify the feature from secret userdata was applied
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				foundSpecPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/spec" {
						foundSpecPatch = true
						spec := op["value"].(map[string]interface{})
						template := spec["template"].(map[string]interface{})
						specMap := template["spec"].(map[string]interface{})
						domain := specMap["domain"].(map[string]interface{})
						cpu := domain["cpu"].(map[string]interface{})
						cpuFeatures := cpu["features"].([]interface{})
						Expect(cpuFeatures).ToNot(BeEmpty())
						break
					}
				}
				Expect(foundSpecPatch).To(BeTrue())
			})
		})
	})
})
