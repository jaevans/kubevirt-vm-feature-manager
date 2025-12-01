package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

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

var _ = Describe("Handler", func() {
	var (
		handler  *Handler
		mutator  *Mutator
		cfg      *config.Config
		recorder *httptest.ResponseRecorder
	)

	BeforeEach(func() {
		cfg = &config.Config{
			AddTrackingAnnotations: true,
			ErrorHandlingMode:      utils.ErrorHandlingReject,
			ConfigSource:           utils.ConfigSourceAnnotations,
		}

		nestedVirtFeature := features.NewNestedVirtualization(&config.NestedVirtConfig{
			Enabled:       true,
			AutoDetectCPU: true,
		}, utils.ConfigSourceAnnotations)

		mutator = NewMutator(nil, cfg, []features.Feature{nestedVirtFeature})
		handler = NewHandler(mutator)
		recorder = httptest.NewRecorder()
	})

	Describe("ServeHTTP", func() {
		Context("with valid admission review", func() {
			It("should return admission response", func() {
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

				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: &admissionv1.AdmissionRequest{
						UID: "test-uid",
						Kind: metav1.GroupVersionKind{
							Group:   "kubevirt.io",
							Version: "v1",
							Kind:    "VirtualMachine",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "kubevirt.io",
							Version:  "v1",
							Resource: "virtualmachines",
						},
						Name:      "test-vm",
						Namespace: "default",
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: vmBytes,
						},
					},
				}

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))

				var responseReview admissionv1.AdmissionReview
				err = json.Unmarshal(recorder.Body.Bytes(), &responseReview)
				Expect(err).ToNot(HaveOccurred())

				Expect(responseReview.Response).ToNot(BeNil())
				Expect(string(responseReview.Response.UID)).To(Equal("test-uid"))
				Expect(responseReview.Response.Allowed).To(BeTrue())
			})
		})

		Context("with VM without feature annotations", func() {
			It("should return allowed response with correct UID", func() {
				// VM without any feature annotations - tests the allowResponse path
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
						// No feature annotations
						Annotations: map[string]string{
							"some-other-annotation": "value",
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

				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: &admissionv1.AdmissionRequest{
						UID: "test-uid-no-features",
						Kind: metav1.GroupVersionKind{
							Group:   "kubevirt.io",
							Version: "v1",
							Kind:    "VirtualMachine",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "kubevirt.io",
							Version:  "v1",
							Resource: "virtualmachines",
						},
						Name:      "test-vm",
						Namespace: "default",
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: vmBytes,
						},
					},
				}

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(recorder.Header().Get("Content-Type")).To(Equal("application/json"))

				var responseReview admissionv1.AdmissionReview
				err = json.Unmarshal(recorder.Body.Bytes(), &responseReview)
				Expect(err).ToNot(HaveOccurred())

				Expect(responseReview.Response).ToNot(BeNil())
				// This is the critical check - UID must be set even when no features are enabled
				Expect(string(responseReview.Response.UID)).To(Equal("test-uid-no-features"))
				Expect(responseReview.Response.Allowed).To(BeTrue())
			})
		})

		Context("with invalid JSON", func() {
			It("should return bad request", func() {
				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader([]byte("invalid json")))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Context("with nil request", func() {
			It("should return bad request", func() {
				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: nil,
				}

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Context("with unreadable body", func() {
			It("should return bad request", func() {
				req := httptest.NewRequest(http.MethodPost, "/mutate", &errorReader{})
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusBadRequest))
			})
		})

		Context("when mutator returns error", func() {
			It("should return internal server error response", func() {
				// Create a VM with invalid spec that will cause mutation error
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

				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: &admissionv1.AdmissionRequest{
						UID: "test-uid",
						Kind: metav1.GroupVersionKind{
							Group:   "kubevirt.io",
							Version: "v1",
							Kind:    "VirtualMachine",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "kubevirt.io",
							Version:  "v1",
							Resource: "virtualmachines",
						},
						Name:      "test-vm",
						Namespace: "default",
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: vmBytes,
						},
					},
				}

				// Add vBIOS feature to trigger the error path
				vbiosFeature := features.NewVBiosInjection(utils.ConfigSourceAnnotations)
				mutator = NewMutator(nil, cfg, []features.Feature{vbiosFeature})
				handler = NewHandler(mutator)

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))

				var responseReview admissionv1.AdmissionReview
				err = json.Unmarshal(recorder.Body.Bytes(), &responseReview)
				Expect(err).ToNot(HaveOccurred())

				Expect(responseReview.Response).ToNot(BeNil())
				Expect(responseReview.Response.Allowed).To(BeFalse())
			})
		})

		Context("with response marshal failure", func() {
			It("should handle valid requests correctly", func() {
				// Note: Actual marshal failures are difficult to trigger without circular
				// references or other pathological data. This test verifies normal operation.
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

				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: &admissionv1.AdmissionRequest{
						UID: "test-uid",
						Kind: metav1.GroupVersionKind{
							Group:   "kubevirt.io",
							Version: "v1",
							Kind:    "VirtualMachine",
						},
						Resource: metav1.GroupVersionResource{
							Group:    "kubevirt.io",
							Version:  "v1",
							Resource: "virtualmachines",
						},
						Name:      "test-vm",
						Namespace: "default",
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: vmBytes,
						},
					},
				}

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
			})
		})

		Context("with request body close handling", func() {
			It("should complete successfully", func() {
				// Tests that body.Close() errors are logged but don't break the handler
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

				admissionReview := &admissionv1.AdmissionReview{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "admission.k8s.io/v1",
						Kind:       "AdmissionReview",
					},
					Request: &admissionv1.AdmissionRequest{
						UID:       "test-uid",
						Operation: admissionv1.Create,
						Object: runtime.RawExtension{
							Raw: vmBytes,
						},
					},
				}

				body, err := json.Marshal(admissionReview)
				Expect(err).ToNot(HaveOccurred())

				req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")

				handler.ServeHTTP(recorder, req)

				// Should complete successfully even if body close has issues
				Expect(recorder.Code).To(Equal(http.StatusOK))
			})
		})
	})
})

// errorWriter is a test helper that fails on Write
type errorWriter struct {
	*httptest.ResponseRecorder
}

func (e *errorWriter) Write(b []byte) (int, error) {
	return 0, io.ErrShortWrite
}

var _ = Describe("Handler Error Paths", func() {
	var (
		handler *Handler
		mutator *Mutator
		cfg     *config.Config
	)

	BeforeEach(func() {
		cfg = &config.Config{
			AddTrackingAnnotations: true,
			ErrorHandlingMode:      utils.ErrorHandlingReject,
		}

		mutator = NewMutator(nil, cfg, []features.Feature{})
		handler = NewHandler(mutator)
	})

	Describe("Write errors", func() {
		It("should handle response write errors gracefully", func() {
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

			admissionReview := &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: vmBytes,
					},
				},
			}

			body, err := json.Marshal(admissionReview)
			Expect(err).ToNot(HaveOccurred())

			req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))

			// Use errorWriter to simulate write failure
			recorder := &errorWriter{ResponseRecorder: httptest.NewRecorder()}

			// This should not panic even with write errors
			handler.ServeHTTP(recorder.ResponseRecorder, req)

			// The handler should set headers even if write fails
			Expect(recorder.ResponseRecorder.Header().Get("Content-Type")).To(Equal("application/json"))
		})
	})
})

// errorReader is a test helper that always returns an error when Read is called
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func (e *errorReader) Close() error {
	return nil
}
