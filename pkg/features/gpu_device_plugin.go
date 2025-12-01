package features

import (
	"context"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

// devicePluginNameRegex validates Kubernetes device plugin resource names.
// Format: domain/resource-name (e.g., nvidia.com/gpu, amd.com/gpu)
// Follows Extended Resource naming convention from Kubernetes.
var devicePluginNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+/[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// GpuDevicePlugin implements GPU device plugin resource allocation for VMs.
// It adds Kubernetes device plugin resources to the VM's resource limits,
// enabling GPU passthrough via device plugins like nvidia.com/gpu.
type GpuDevicePlugin struct {
	configSource utils.ConfigSource
}

// NewGpuDevicePlugin creates a new GpuDevicePlugin instance.
func NewGpuDevicePlugin(configSource utils.ConfigSource) *GpuDevicePlugin {
	return &GpuDevicePlugin{
		configSource: configSource,
	}
}

// Name returns the feature name.
func (f *GpuDevicePlugin) Name() string {
	return utils.FeatureGpuDevicePlugin
}

// IsEnabled checks if the GPU device plugin feature is enabled for this VM.
func (f *GpuDevicePlugin) IsEnabled(vm *kubevirtv1.VirtualMachine) bool {
	pluginName, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationGpuDevicePlugin)
	return exists && pluginName != ""
}

// Validate ensures the device plugin name is valid.
func (f *GpuDevicePlugin) Validate(ctx context.Context, vm *kubevirtv1.VirtualMachine, k8sClient client.Client) error {
	pluginName, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationGpuDevicePlugin)
	if !exists {
		return nil
	}

	if pluginName == "" {
		return fmt.Errorf("GPU device plugin name cannot be empty")
	}

	if !devicePluginNameRegex.MatchString(pluginName) {
		return fmt.Errorf("invalid device plugin name %q: must be in format 'domain/resource' (e.g., nvidia.com/gpu)", pluginName)
	}

	return nil
}

// Apply adds the GPU device plugin resource to the VM's resource limits.
func (f *GpuDevicePlugin) Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, k8sClient client.Client) (*MutationResult, error) {
	result := &MutationResult{
		Applied:     false,
		Annotations: make(map[string]string),
	}

	if !f.IsEnabled(vm) {
		return result, nil
	}

	// Validate before applying
	if err := f.Validate(ctx, vm, k8sClient); err != nil {
		return result, err
	}

	if vm.Spec.Template == nil {
		return result, fmt.Errorf("VM template is nil")
	}

	pluginName, _ := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationGpuDevicePlugin)

	// Initialize resources if needed
	if vm.Spec.Template.Spec.Domain.Resources.Limits == nil {
		vm.Spec.Template.Spec.Domain.Resources.Limits = make(corev1.ResourceList)
	}

	// Add GPU resource limit (quantity of 1)
	// Note: We don't override if the resource already exists
	resourceName := corev1.ResourceName(pluginName)
	if _, exists := vm.Spec.Template.Spec.Domain.Resources.Limits[resourceName]; !exists {
		vm.Spec.Template.Spec.Domain.Resources.Limits[resourceName] = resource.MustParse("1")
	}

	result.Applied = true
	result.Annotations[utils.AnnotationGpuDevicePluginApplied] = pluginName

	return result, nil
}
