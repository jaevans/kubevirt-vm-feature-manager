package features

import (
	"context"
	"fmt"
	"runtime"

	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

// NestedVirtualization implements the nested virtualization feature
type NestedVirtualization struct {
	config       *config.NestedVirtConfig
	configSource utils.ConfigSource
}

// NewNestedVirtualization creates a new NestedVirtualization feature
func NewNestedVirtualization(cfg *config.NestedVirtConfig, configSource utils.ConfigSource) *NestedVirtualization {
	return &NestedVirtualization{
		config:       cfg,
		configSource: configSource,
	}
}

// Name returns the feature name
func (f *NestedVirtualization) Name() string {
	return utils.FeatureNestedVirt
}

// IsEnabled checks if nested virtualization is requested via annotations or labels
func (f *NestedVirtualization) IsEnabled(vm *kubevirtv1.VirtualMachine) bool {
	if !f.config.Enabled {
		return false
	}

	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationNestedVirt)
	return exists && utils.IsTruthyValue(value)
}

// Apply enables nested virtualization by adding CPU features
func (f *NestedVirtualization) Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, _ client.Client) (*MutationResult, error) {
	logger := log.FromContext(ctx)
	result := NewMutationResult()

	if !f.IsEnabled(vm) {
		return result, nil
	}

	logger.Info("Applying nested virtualization feature", "vm", vm.Name)

	// Determine CPU feature to add (AMD SVM or Intel VMX)
	cpuFeature := f.detectCPUFeature()

	// Initialize domain if needed
	if vm.Spec.Template == nil {
		vm.Spec.Template = &kubevirtv1.VirtualMachineInstanceTemplateSpec{}
	}
	if vm.Spec.Template.Spec.Domain.CPU == nil {
		vm.Spec.Template.Spec.Domain.CPU = &kubevirtv1.CPU{}
	}

	// Add CPU feature
	feature := kubevirtv1.CPUFeature{
		Name:   cpuFeature,
		Policy: "require",
	}

	// Check if feature already exists
	featureExists := false
	for _, existing := range vm.Spec.Template.Spec.Domain.CPU.Features {
		if existing.Name == cpuFeature {
			featureExists = true
			break
		}
	}

	if !featureExists {
		vm.Spec.Template.Spec.Domain.CPU.Features = append(
			vm.Spec.Template.Spec.Domain.CPU.Features,
			feature,
		)
	}

	// Mark as applied
	result.Applied = true
	result.AddAnnotation(utils.AnnotationNestedVirtApplied, "true")
	result.AddMessage(fmt.Sprintf("Enabled nested virtualization with %s CPU feature", cpuFeature))

	logger.Info("Nested virtualization applied successfully",
		"vm", vm.Name,
		"cpuFeature", cpuFeature)

	return result, nil
}

// Validate performs basic validation
func (f *NestedVirtualization) Validate(_ context.Context, vm *kubevirtv1.VirtualMachine, _ client.Client) error {
	// Check if config value is present
	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationNestedVirt)
	if !exists {
		return nil
	}

	// If config value exists, validate it
	if value != "enabled" {
		return fmt.Errorf("invalid value for %s: %s (expected 'enabled')",
			utils.AnnotationNestedVirt, value)
	}

	return nil
}

// detectCPUFeature determines which CPU feature to use based on platform
func (f *NestedVirtualization) detectCPUFeature() string {
	if !f.config.AutoDetectCPU {
		// Default to AMD if auto-detect is disabled
		return utils.CPUFeatureSVM
	}

	// In a real implementation, you might read /proc/cpuinfo or query the node
	// For now, we'll use a simple heuristic based on GOARCH
	// This is a placeholder - actual detection would need to query the cluster nodes
	arch := runtime.GOARCH

	if arch == "amd64" || arch == "x86_64" {
		// Default to AMD SVM for x86_64
		// TODO: In production, this should query actual node CPU capabilities
		return utils.CPUFeatureSVM
	}

	// Fallback to AMD
	return utils.CPUFeatureSVM
}
