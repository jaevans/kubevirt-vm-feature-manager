package features

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

// ConfigMap name validation: DNS subdomain (RFC 1123)
// lowercase alphanumeric characters, '-' or '.', start and end with alphanumeric
var configMapNameRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// Container image reference validation (simplified)
var imageRefRegex = regexp.MustCompile(`^[a-zA-Z0-9._/-]+:[a-zA-Z0-9._-]+$`)

// HookSidecar represents a KubeVirt hook sidecar configuration
type HookSidecar struct {
	Image           string   `json:"image"`
	ImagePullPolicy string   `json:"imagePullPolicy,omitempty"`
	Args            []string `json:"args,omitempty"`
}

// VBiosInjection implements vBIOS injection via KubeVirt hook sidecar
type VBiosInjection struct {
	configSource string
}

// NewVBiosInjection creates a new VBiosInjection feature
func NewVBiosInjection(configSource string) *VBiosInjection {
	return &VBiosInjection{
		configSource: configSource,
	}
}

// Name returns the feature name
func (f *VBiosInjection) Name() string {
	return utils.FeatureVBiosInjection
}

// IsEnabled checks if vBIOS injection is requested via annotations or labels
func (f *VBiosInjection) IsEnabled(vm *kubevirtv1.VirtualMachine) bool {
	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationVBiosInjection)
	return exists && value != ""
}

// Validate performs validation of vBIOS injection configuration
func (f *VBiosInjection) Validate(_ context.Context, vm *kubevirtv1.VirtualMachine, _ client.Client) error {
	configMapName, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationVBiosInjection)
	if !exists {
		return nil
	}

	// Validate ConfigMap name is not empty
	if configMapName == "" {
		return fmt.Errorf("empty ConfigMap name in %s configuration key", utils.AnnotationVBiosInjection)
	}

	// Validate ConfigMap name length (max 253 characters per DNS subdomain spec)
	if len(configMapName) > 253 {
		return fmt.Errorf("ConfigMap name too long (max 253 characters): %s", configMapName)
	}

	// Validate ConfigMap name format (DNS subdomain)
	if !configMapNameRegex.MatchString(configMapName) {
		return fmt.Errorf("invalid ConfigMap name format: %s (must be a valid DNS subdomain)", configMapName)
	}

	// Validate sidecar image if provided (always read from annotations since it's a secondary config)
	annotations := vm.GetAnnotations()
	if annotations != nil {
		if sidecarImage, ok := annotations[utils.AnnotationSidecarImage]; ok && sidecarImage != "" {
			if !imageRefRegex.MatchString(sidecarImage) {
				return fmt.Errorf("invalid sidecar image reference: %s", sidecarImage)
			}
		}
	}

	return nil
}

// Apply adds vBIOS injection hook sidecar to the VM
func (f *VBiosInjection) Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, _ client.Client) (*MutationResult, error) {
	logger := log.FromContext(ctx)
	result := NewMutationResult()

	configMapName, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationVBiosInjection)
	if !exists || configMapName == "" {
		return result, nil
	}

	logger.Info("Applying vBIOS injection feature", "vm", vm.Name, "configMap", configMapName)

	// Validate template exists
	if vm.Spec.Template == nil {
		return result, fmt.Errorf("VM template is nil")
	}

	// Validate ConfigMap name
	if err := f.Validate(ctx, vm, nil); err != nil {
		return result, err
	}

	// Determine sidecar image to use (always read from annotations since it's a secondary config)
	sidecarImage := utils.DefaultSidecarImage
	annotations := vm.GetAnnotations()
	if annotations != nil {
		if customImage, ok := annotations[utils.AnnotationSidecarImage]; ok && customImage != "" {
			sidecarImage = customImage
			logger.Info("Using custom sidecar image", "image", sidecarImage)
		}
	}

	// Add vBIOS volume if not already present
	if err := f.addVBiosVolume(vm, configMapName); err != nil {
		return result, err
	}

	// Add hook sidecar annotation
	if err := f.addHookSidecar(vm, sidecarImage); err != nil {
		return result, err
	}

	// Mark as applied
	result.Applied = true
	result.AddAnnotation(utils.AnnotationVBiosInjectionApplied, configMapName)
	result.AddMessage(fmt.Sprintf("Configured vBIOS injection with ConfigMap %s", configMapName))

	logger.Info("vBIOS injection applied successfully",
		"vm", vm.Name,
		"configMap", configMapName,
		"sidecarImage", sidecarImage)

	return result, nil
}

// addVBiosVolume adds the vBIOS ConfigMap volume to the VM spec
func (f *VBiosInjection) addVBiosVolume(vm *kubevirtv1.VirtualMachine, configMapName string) error {
	// Check if volume already exists
	for _, vol := range vm.Spec.Template.Spec.Volumes {
		if vol.Name == "vbios-rom" {
			// Volume already exists, don't add duplicate
			return nil
		}
	}

	// Add the volume
	vbiosVolume := kubevirtv1.Volume{
		Name: "vbios-rom",
		VolumeSource: kubevirtv1.VolumeSource{
			ConfigMap: &kubevirtv1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}

	vm.Spec.Template.Spec.Volumes = append(vm.Spec.Template.Spec.Volumes, vbiosVolume)
	return nil
}

// addHookSidecar adds the KubeVirt hook sidecar annotation
func (f *VBiosInjection) addHookSidecar(vm *kubevirtv1.VirtualMachine, sidecarImage string) error {
	// Initialize template annotations if needed
	if vm.Spec.Template.ObjectMeta.Annotations == nil {
		vm.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
	}

	// Check if hook sidecar already exists
	if existingHook, exists := vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey]; exists && existingHook != "" {
		// Hook already configured, don't override
		return nil
	}

	// Create hook sidecar configuration
	hookSidecar := HookSidecar{
		Image:           sidecarImage,
		ImagePullPolicy: "IfNotPresent",
		Args: []string{
			"--version", utils.SidecarHookVersion,
			"--hook-type", utils.SidecarHookType,
		},
	}

	// Marshal to JSON array (KubeVirt expects an array of sidecars)
	hookJSON, err := json.Marshal([]HookSidecar{hookSidecar})
	if err != nil {
		return fmt.Errorf("failed to marshal hook sidecar configuration: %w", err)
	}

	vm.Spec.Template.ObjectMeta.Annotations[utils.HookAnnotationKey] = string(hookJSON)
	return nil
}
