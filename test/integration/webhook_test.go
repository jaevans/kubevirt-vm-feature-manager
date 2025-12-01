package integration_test

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

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/webhook"
)

var _ = Describe("Webhook Integration Tests", func() {
	var (
		testCtx    context.Context
		testCancel context.CancelFunc
		cfg        *config.Config
		mutator    *webhook.Mutator
	)

	BeforeEach(func() {
		testCtx, testCancel = context.WithCancel(ctx)

		// Create test config
		cfg = &config.Config{
			AddTrackingAnnotations: true,
			ErrorHandlingMode:      utils.ErrorHandlingReject,
			Features: config.FeaturesConfig{
				NestedVirtualization: config.NestedVirtConfig{
					Enabled:       true,
					AutoDetectCPU: true,
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

		// Create features
		allFeatures := []features.Feature{
			features.NewNestedVirtualization(&cfg.Features.NestedVirtualization, utils.ConfigSourceAnnotations),
			features.NewPciPassthrough(utils.ConfigSourceAnnotations),
			features.NewVBiosInjection(utils.ConfigSourceAnnotations),
			features.NewGpuDevicePlugin(utils.ConfigSourceAnnotations),
		}

		// Create mutator with real Kubernetes client
		mutator = webhook.NewMutator(k8sClient, cfg, allFeatures)
	})

	AfterEach(func() {
		testCancel()
	})

	Describe("End-to-End Webhook Flow", func() {
		Context("with nested virtualization annotation", func() {
			It("should mutate VM through full webhook path", func() {
				vm := createBasicVM("nested-virt-e2e", "integration-test", map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-1",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify patch contains CPU features
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())
				Expect(patchOps).ToNot(BeEmpty())
			})
		})

		Context("with GPU device plugin annotation", func() {
			It("should add GPU resources through webhook", func() {
				vm := createBasicVM("gpu-e2e", "integration-test", map[string]string{
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-2",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())
			})
		})

		Context("with PCI passthrough annotation", func() {
			It("should add PCI devices through webhook", func() {
				vm := createBasicVM("pci-e2e", "integration-test", map[string]string{
					utils.AnnotationPciPassthrough: `{"devices":["0000:03:00.0"]}`,
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-3",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())
			})
		})

		Context("with vBIOS injection annotation", func() {
			var configMap *corev1.ConfigMap

			BeforeEach(func() {
				configMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vbios-e2e",
						Namespace: "integration-test",
					},
					BinaryData: map[string][]byte{
						utils.VBiosConfigMapKey: []byte("fake-vbios-data"),
					},
				}
				err := k8sClient.Create(testCtx, configMap)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				if configMap != nil {
					_ = k8sClient.Delete(testCtx, configMap)
				}
			})

			It("should add vBIOS volume and hook through webhook", func() {
				vm := createBasicVM("vbios-e2e", "integration-test", map[string]string{
					utils.AnnotationVBiosInjection: "vbios-e2e",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-4",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())
			})
		})

		Context("with multiple features enabled", func() {
			var configMap *corev1.ConfigMap

			BeforeEach(func() {
				configMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-vbios",
						Namespace: "integration-test",
					},
					BinaryData: map[string][]byte{
						utils.VBiosConfigMapKey: []byte("fake-vbios-data"),
					},
				}
				err := k8sClient.Create(testCtx, configMap)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				if configMap != nil {
					_ = k8sClient.Delete(testCtx, configMap)
				}
			})

			It("should apply all features through webhook in single request", func() {
				vm := createBasicVM("multi-feature-e2e", "integration-test", map[string]string{
					utils.AnnotationNestedVirt:      "enabled",
					utils.AnnotationGpuDevicePlugin: "nvidia.com/gpu",
					utils.AnnotationPciPassthrough:  `{"devices":["0000:03:00.0"]}`,
					utils.AnnotationVBiosInjection:  "multi-vbios",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-5",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Patch).ToNot(BeNil())

				// Verify patch is valid JSON
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())
				Expect(patchOps).To(HaveLen(2)) // spec and annotations patches
			})
		})
	})

	Describe("Webhook Error Handling", func() {
		Context("with invalid PCI address", func() {
			It("should reject VM with validation error", func() {
				vm := createBasicVM("invalid-pci", "integration-test", map[string]string{
					utils.AnnotationPciPassthrough: `{"devices":["invalid-address"]}`,
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-6",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(ContainSubstring("invalid PCI address"))
			})
		})

		Context("with invalid GPU device plugin name", func() {
			It("should reject VM with validation error", func() {
				vm := createBasicVM("invalid-gpu", "integration-test", map[string]string{
					utils.AnnotationGpuDevicePlugin: "invalid name!",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-7",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(ContainSubstring("invalid device plugin name"))
			})
		})

		Context("with nil template", func() {
			It("should reject VM with application error", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nil-template",
						Namespace: "integration-test",
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
					UID:       "test-uid-8",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
				Expect(response.Result.Message).To(ContainSubstring("template is nil"))
			})
		})
	})

	Describe("Webhook Error Handling Modes", func() {
		Context("with allow-and-log error mode", func() {
			BeforeEach(func() {
				cfg.ErrorHandlingMode = utils.ErrorHandlingAllowAndLog
				allFeatures := []features.Feature{
					features.NewVBiosInjection(utils.ConfigSourceAnnotations),
				}
				mutator = webhook.NewMutator(k8sClient, cfg, allFeatures)
			})

			It("should allow VM despite feature error", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "allow-error",
						Namespace: "integration-test",
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
					UID:       "test-uid-9",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())
				Expect(response.Result.Message).To(ContainSubstring("allowed"))
			})
		})

		Context("with reject error mode", func() {
			BeforeEach(func() {
				cfg.ErrorHandlingMode = utils.ErrorHandlingReject
				allFeatures := []features.Feature{
					features.NewVBiosInjection(utils.ConfigSourceAnnotations),
				}
				mutator = webhook.NewMutator(k8sClient, cfg, allFeatures)
			})

			It("should reject VM on feature error", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "reject-error",
						Namespace: "integration-test",
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
					UID:       "test-uid-10",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeFalse())
			})
		})
	})

	Describe("Tracking Annotations", func() {
		Context("with tracking enabled", func() {
			It("should include tracking annotations in patch", func() {
				vm := createBasicVM("tracking-enabled", "integration-test", map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-11",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())

				// Parse patch to verify tracking annotations
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Look for annotations patch operation and verify it contains tracking
				foundAnnotationsPatch := false
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						foundAnnotationsPatch = true
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue(), "annotations patch value should be a map")
						// Verify the tracking annotation is present
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirtApplied))
						Expect(annotations[utils.AnnotationNestedVirtApplied]).To(Equal("true"))
						break
					}
				}
				Expect(foundAnnotationsPatch).To(BeTrue())
			})
		})

		Context("with tracking disabled", func() {
			BeforeEach(func() {
				cfg.AddTrackingAnnotations = false
				allFeatures := []features.Feature{
					features.NewNestedVirtualization(&cfg.Features.NestedVirtualization, utils.ConfigSourceAnnotations),
				}
				mutator = webhook.NewMutator(k8sClient, cfg, allFeatures)
			})

			It("should still apply feature but not add tracking annotations", func() {
				vm := createBasicVM("tracking-disabled", "integration-test", map[string]string{
					utils.AnnotationNestedVirt: "enabled",
				})

				vmBytes, err := json.Marshal(vm)
				Expect(err).ToNot(HaveOccurred())

				req := &admissionv1.AdmissionRequest{
					UID:       "test-uid-12",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				}

				response, err := mutator.Handle(testCtx, req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
				Expect(response.Allowed).To(BeTrue())

				// Parse patch to verify tracking annotations are NOT added
				var patchOps []map[string]interface{}
				err = json.Unmarshal(response.Patch, &patchOps)
				Expect(err).ToNot(HaveOccurred())

				// Verify that if annotations patch exists, it doesn't have tracking
				for _, op := range patchOps {
					if path, ok := op["path"].(string); ok && path == "/metadata/annotations" {
						annotations, ok := op["value"].(map[string]interface{})
						Expect(ok).To(BeTrue())
						// Original annotation should be present
						Expect(annotations).To(HaveKey(utils.AnnotationNestedVirt))
						// But NOT the tracking annotation
						Expect(annotations).ToNot(HaveKey(utils.AnnotationNestedVirtApplied))
					}
				}
			})
		})
	})
})
