package features

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

// PCI address format: DDDD:BB:DD.F (domain:bus:device.function)
var pciAddressRegex = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)

// PCIPassthroughSpec defines the structure of the PCI passthrough annotation
type PCIPassthroughSpec struct {
	Devices []string `json:"devices"`
}

// PciPassthrough implements PCI device passthrough feature
type PciPassthrough struct {
	configSource string
}

// NewPciPassthrough creates a new PciPassthrough feature
func NewPciPassthrough(configSource string) *PciPassthrough {
	return &PciPassthrough{
		configSource: configSource,
	}
}

// Name returns the feature name
func (f *PciPassthrough) Name() string {
	return utils.FeaturePciPassthrough
}

// IsEnabled checks if PCI passthrough is requested via annotations or labels
func (f *PciPassthrough) IsEnabled(vm *kubevirtv1.VirtualMachine) bool {
	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationPciPassthrough)
	return exists && value != ""
}

// Validate performs validation of PCI passthrough configuration
func (f *PciPassthrough) Validate(_ context.Context, vm *kubevirtv1.VirtualMachine, _ client.Client) error {
	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationPciPassthrough)
	if !exists {
		return nil
	}

	// Parse the JSON spec
	var spec PCIPassthroughSpec
	if err := json.Unmarshal([]byte(value), &spec); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", utils.AnnotationPciPassthrough, err)
	}

	// Validate devices array is not empty
	if len(spec.Devices) == 0 {
		return fmt.Errorf("no devices specified in %s", utils.AnnotationPciPassthrough)
	}

	// Check for duplicates
	seen := make(map[string]bool)
	for _, device := range spec.Devices {
		if seen[device] {
			return fmt.Errorf("duplicate PCI device address: %s", device)
		}
		seen[device] = true

		// Validate PCI address format
		if !pciAddressRegex.MatchString(device) {
			return fmt.Errorf("invalid PCI address format: %s (expected DDDD:BB:DD.F)", device)
		}
	}

	return nil
}

// Apply adds PCI devices to the VM spec
func (f *PciPassthrough) Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, cl client.Client) (*MutationResult, error) {
	logger := log.FromContext(ctx)
	result := NewMutationResult()

	value, exists := utils.GetConfigValue(f.configSource, vm.GetAnnotations(), vm.GetLabels(), utils.AnnotationPciPassthrough)
	if !exists || value == "" {
		return result, nil
	}

	logger.Info("Applying PCI passthrough feature", "vm", vm.Name)

	// Validate template exists
	if vm.Spec.Template == nil {
		return result, fmt.Errorf("VM template is nil")
	}

	// Parse the JSON spec
	var spec PCIPassthroughSpec
	if err := json.Unmarshal([]byte(value), &spec); err != nil {
		return result, fmt.Errorf("invalid JSON in %s: %w", utils.AnnotationPciPassthrough, err)
	}

	// Get existing host devices to check for duplicates
	existingDevices := make(map[string]bool)
	if vm.Spec.Template.Spec.Domain.Devices.HostDevices != nil {
		for _, hd := range vm.Spec.Template.Spec.Domain.Devices.HostDevices {
			existingDevices[hd.DeviceName] = true
		}
	}

	// Add each PCI device
	var addedDevices []string
	for i, pciAddr := range spec.Devices {
		// Convert PCI address to KubeVirt device name format
		// 0000:00:02.0 -> pci_0000_00_02_0
		deviceName := "pci_" + strings.ReplaceAll(strings.ReplaceAll(pciAddr, ":", "_"), ".", "_")

		// Skip if already exists
		if existingDevices[deviceName] {
			logger.Info("PCI device already exists, skipping", "device", pciAddr)
			continue
		}

		// Add the host device
		hostDevice := kubevirtv1.HostDevice{
			Name:       fmt.Sprintf("pci-device-%d", i),
			DeviceName: deviceName,
		}

		vm.Spec.Template.Spec.Domain.Devices.HostDevices = append(
			vm.Spec.Template.Spec.Domain.Devices.HostDevices,
			hostDevice,
		)

		addedDevices = append(addedDevices, pciAddr)
		result.Applied = true
	}

	if result.Applied {
		// Add tracking annotation with the list of devices
		devicesJSON, _ := json.Marshal(addedDevices)
		result.AddAnnotation(utils.AnnotationPciPassthroughApplied, string(devicesJSON))
		logger.Info("Successfully applied PCI passthrough", "devices", addedDevices)
	}

	return result, nil
}
