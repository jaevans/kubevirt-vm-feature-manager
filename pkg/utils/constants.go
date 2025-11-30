// Package utils provides utility constants and helper functions for the VM Feature Manager.
package utils

import "strings"

const (
	// AnnotationNestedVirt enables nested virtualization for a VM
	AnnotationNestedVirt = "vm-feature-manager.io/nested-virt"
	// AnnotationVBiosInjection specifies the ConfigMap containing the vBIOS blob
	AnnotationVBiosInjection = "vm-feature-manager.io/vbios-injection"
	// AnnotationPciPassthrough specifies PCI devices for passthrough (JSON array)
	AnnotationPciPassthrough = "vm-feature-manager.io/pci-passthrough"
	// AnnotationGpuDevicePlugin specifies the GPU device plugin to use
	AnnotationGpuDevicePlugin = "vm-feature-manager.io/gpu-device-plugin"
	// AnnotationSidecarImage overrides the default sidecar image for vBIOS injection
	AnnotationSidecarImage = "vm-feature-manager.io/sidecar-image"

	// AnnotationNestedVirtApplied tracks successful nested virt application
	AnnotationNestedVirtApplied = "vm-feature-manager.io/nested-virt-applied"
	// AnnotationVBiosInjectionApplied tracks successful vBIOS injection
	AnnotationVBiosInjectionApplied = "vm-feature-manager.io/vbios-injection-applied"
	// AnnotationPciPassthroughApplied tracks successful PCI passthrough
	AnnotationPciPassthroughApplied = "vm-feature-manager.io/pci-passthrough-applied"
	// AnnotationGpuDevicePluginApplied tracks successful GPU device plugin
	AnnotationGpuDevicePluginApplied = "vm-feature-manager.io/gpu-device-plugin-applied"

	// AnnotationNestedVirtError tracks nested virt errors
	AnnotationNestedVirtError = "vm-feature-manager.io/nested-virt-error"
	// AnnotationVBiosInjectionError tracks vBIOS injection errors
	AnnotationVBiosInjectionError = "vm-feature-manager.io/vbios-injection-error"
	// AnnotationPciPassthroughError tracks PCI passthrough errors
	AnnotationPciPassthroughError = "vm-feature-manager.io/pci-passthrough-error"
	// AnnotationGpuDevicePluginError tracks GPU device plugin errors
	AnnotationGpuDevicePluginError = "vm-feature-manager.io/gpu-device-plugin-error"

	// FeatureNestedVirt is the name for the nested virtualization feature
	FeatureNestedVirt = "nested-virt"
	// FeatureVBiosInjection is the name for the vBIOS injection feature
	FeatureVBiosInjection = "vbios-injection"
	// FeaturePciPassthrough is the name for the PCI passthrough feature
	FeaturePciPassthrough = "pci-passthrough"
	// FeatureGpuDevicePlugin is the name for the GPU device plugin feature
	FeatureGpuDevicePlugin = "gpu-device-plugin"

	// CPUFeatureSVM is the AMD SVM CPU feature name for nested virtualization
	CPUFeatureSVM = "svm"
	// CPUFeatureVMX is the Intel VMX CPU feature name for nested virtualization
	CPUFeatureVMX = "vmx"

	// DefaultSidecarImage is the default KubeVirt sidecar-shim image for vBIOS injection
	DefaultSidecarImage = "registry.k8s.io/kubevirt/sidecar-shim:v1.4.0"
	// SidecarHookVersion is the hook sidecar API version
	SidecarHookVersion = "v1alpha2"
	// SidecarHookType is the type of hook to use
	SidecarHookType = "onDefineDomain"
	// VBiosConfigMapKey is the key name for vBIOS data in ConfigMaps
	VBiosConfigMapKey = "rom"
	// HookAnnotationKey is the KubeVirt annotation for hook sidecars
	HookAnnotationKey = "hooks.kubevirt.io/hookSidecars"

	// ErrorHandlingReject causes the webhook to reject VMs when feature application fails
	ErrorHandlingReject = "reject"
	// ErrorHandlingAllowAndLog allows VMs through but logs feature application failures
	ErrorHandlingAllowAndLog = "allow-and-log"
	// ErrorHandlingStripLabel removes the failing feature annotation and allows the VM through
	ErrorHandlingStripLabel = "strip-label"
)

// IsTruthyValue checks if a string value represents a boolean "true"
// Accepts: "true", "enabled", "yes", "1" (case-insensitive)
func IsTruthyValue(value string) bool {
	switch strings.ToLower(value) {
	case "true", "enabled", "yes", "1":
		return true
	default:
		return false
	}
}
