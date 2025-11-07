package webhook

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"

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
				})
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
				})
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
				})
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
				})
				gpuFeature := features.NewGpuDevicePlugin()
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

				gpuFeature := features.NewGpuDevicePlugin()
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

				vbiosFeature := features.NewVBiosInjection()
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
				})
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

				vbiosFeature := features.NewVBiosInjection()
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

				vbiosFeature := features.NewVBiosInjection()
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

				vbiosFeature := features.NewVBiosInjection()
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
			})
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
			})
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

				vbiosFeature := features.NewVBiosInjection()
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
				})
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

				gpuFeature := features.NewGpuDevicePlugin()
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
})
