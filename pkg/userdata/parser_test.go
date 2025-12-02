package userdata_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/userdata"
)

var _ = Describe("Userdata Parser", func() {
	var (
		ctx        context.Context
		fakeClient client.Client
		parser     *userdata.Parser
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme := setupScheme()
		fakeClient = fake.NewClientBuilder().WithScheme(scheme).Build()
		parser = userdata.NewParser(fakeClient)
	})

	Describe("ParseFeatures", func() {
		Context("with plain text userdata", func() {
			It("should extract single feature directive", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
x_kubevirt_features:
  nested_virt: enabled
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

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})

			It("should extract multiple feature directives", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
x_kubevirt_features:
  nested_virt: enabled
  gpu_device_plugin: nvidia.com/gpu
  pci_passthrough:
    devices:
      - "0000:00:02.0"
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

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveLen(3))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/gpu-device-plugin", "nvidia.com/gpu"))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/pci-passthrough", `{"devices":["0000:00:02.0"]}`))
			})

			It("should handle boolean values", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
x_kubevirt_features:
  nested_virt: true
  gpu_device_plugin: nvidia.com/gpu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveLen(2))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/gpu-device-plugin", "nvidia.com/gpu"))
			})

			It("should ignore other cloud-config keys", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
hostname: test-vm
x_kubevirt_features:
  nested_virt: enabled
users:
  - name: ubuntu
packages:
  - vim
`,
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveLen(1))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})
		})

		Context("with base64-encoded userdata", func() {
			It("should decode and extract features", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataBase64: "I2Nsb3VkLWNvbmZpZwp4X2t1YmV2aXJ0X2ZlYXR1cmVzOgogIG5lc3RlZF92aXJ0OiBlbmFibGVkCnVzZXJzOgogIC0gbmFtZTogdWJ1bnR1Cg==",
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})

			It("should handle invalid base64", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataBase64: "not-valid-base64!!!",
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(BeEmpty())
			})
		})

		Context("with secret reference", func() {
			It("should fetch and parse userdata from secret", func() {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"userdata": []byte(`#cloud-config
x_kubevirt_features:
  nested_virt: enabled
users:
  - name: ubuntu
`),
					},
				}
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "test-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})

			It("should try multiple common keys in secret", func() {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"user-data": []byte(`#cloud-config
x_kubevirt_features:
  nested_virt: enabled
`),
					},
				}
				Expect(fakeClient.Create(ctx, secret)).To(Succeed())

				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "test-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})

			It("should handle missing secret", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserDataSecretRef: &corev1.LocalObjectReference{
													Name: "missing-secret",
												},
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(BeEmpty())
			})
		})

		Context("with CloudInitConfigDrive", func() {
			It("should extract features from ConfigDrive userdata", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitConfigDrive: &kubevirtv1.CloudInitConfigDriveSource{
												UserData: `#cloud-config
x_kubevirt_features:
  nested_virt: enabled
`,
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
			})
		})

		Context("with multiple volumes", func() {
			It("should merge features from all volumes", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{
									{
										Name: "cloudinit1",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{
												UserData: `#cloud-config
x_kubevirt_features:
  nested_virt: enabled
`,
											},
										},
									},
									{
										Name: "cloudinit2",
										VolumeSource: kubevirtv1.VolumeSource{
											CloudInitConfigDrive: &kubevirtv1.CloudInitConfigDriveSource{
												UserData: `#cloud-config
x_kubevirt_features:
  gpu_device_plugin: nvidia.com/gpu
`,
											},
										},
									},
								},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(HaveLen(2))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/nested-virt", "enabled"))
				Expect(features).To(HaveKeyWithValue("vm-feature-manager.io/gpu-device-plugin", "nvidia.com/gpu"))
			})
		})

		Context("with no userdata", func() {
			It("should return empty map for VM without template", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(BeEmpty())
			})

			It("should return empty map for VM without volumes", func() {
				vm := &kubevirtv1.VirtualMachine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vm",
						Namespace: "default",
					},
					Spec: kubevirtv1.VirtualMachineSpec{
						Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
							Spec: kubevirtv1.VirtualMachineInstanceSpec{
								Volumes: []kubevirtv1.Volume{},
							},
						},
					},
				}

				features, err := parser.ParseFeatures(ctx, vm)
				Expect(err).NotTo(HaveOccurred())
				Expect(features).To(BeEmpty())
			})
		})
	})
})

// setupScheme creates a scheme with required types for testing
func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = kubevirtv1.AddToScheme(scheme)
	return scheme
}
