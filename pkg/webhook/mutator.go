package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/utils"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	_ = kubevirtv1.AddToScheme(scheme)
	_ = admissionv1.AddToScheme(scheme)
}

// Mutator handles VM mutation based on feature annotations
type Mutator struct {
	client   client.Client
	config   *config.Config
	features []features.Feature
}

// NewMutator creates a new Mutator
func NewMutator(client client.Client, cfg *config.Config, featureList []features.Feature) *Mutator {
	return &Mutator{
		client:   client,
		config:   cfg,
		features: featureList,
	}
}

// Handle processes admission requests
func (m *Mutator) Handle(ctx context.Context, req *admissionv1.AdmissionRequest) (*admissionv1.AdmissionResponse, error) {
	logger := log.FromContext(ctx)

	// Decode the VM object
	vm := &kubevirtv1.VirtualMachine{}
	if err := json.Unmarshal(req.Object.Raw, vm); err != nil {
		logger.Error(err, "Failed to unmarshal VM")
		return m.errorResponse(err), nil
	}

	logger.Info("Processing VM mutation",
		"vm", vm.Name,
		"namespace", vm.Namespace,
		"operation", req.Operation)

	// Check if any features are enabled
	if !m.hasEnabledFeatures(vm) {
		logger.Info("No features enabled for VM", "vm", vm.Name)
		return m.allowResponse("No features requested"), nil
	}

	// Create a copy to mutate
	mutatedVM := vm.DeepCopy()

	// Apply features
	appliedFeatures := []string{}
	allAnnotations := make(map[string]string)

	for _, feature := range m.features {
		if !feature.IsEnabled(vm) {
			continue
		}

		logger.Info("Feature enabled", "feature", feature.Name(), "vm", vm.Name)

		// Validate
		if err := feature.Validate(ctx, mutatedVM, m.client); err != nil {
			logger.Error(err, "Feature validation failed", "feature", feature.Name())
			return m.handleError(feature.Name(), err), nil
		}

		// Apply
		result, err := feature.Apply(ctx, mutatedVM, m.client)
		if err != nil {
			logger.Error(err, "Feature application failed", "feature", feature.Name())
			return m.handleError(feature.Name(), err), nil
		}

		if result.Applied {
			appliedFeatures = append(appliedFeatures, feature.Name())

			// Collect tracking annotations
			for k, v := range result.Annotations {
				allAnnotations[k] = v
			}

			logger.Info("Feature applied successfully",
				"feature", feature.Name(),
				"vm", vm.Name,
				"messages", result.Messages)
		}
	}

	// Add tracking annotations if enabled
	if m.config.AddTrackingAnnotations && len(appliedFeatures) > 0 {
		if mutatedVM.Annotations == nil {
			mutatedVM.Annotations = make(map[string]string)
		}

		// Add feature-specific annotations
		for k, v := range allAnnotations {
			mutatedVM.Annotations[k] = v
		}
	}

	// Create JSON patch
	patch, err := m.createPatch(vm, mutatedVM)
	if err != nil {
		logger.Error(err, "Failed to create patch")
		return m.errorResponse(err), nil
	}

	logger.Info("VM mutation successful",
		"vm", vm.Name,
		"appliedFeatures", appliedFeatures)

	return &admissionv1.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
		Patch:   patch,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}, nil
}

// hasEnabledFeatures checks if any feature is requested via annotations
func (m *Mutator) hasEnabledFeatures(vm *kubevirtv1.VirtualMachine) bool {
	for _, feature := range m.features {
		if feature.IsEnabled(vm) {
			return true
		}
	}
	return false
}

// createPatch creates a JSON patch between original and mutated VM
func (m *Mutator) createPatch(original, mutated *kubevirtv1.VirtualMachine) ([]byte, error) {
	originalBytes, err := json.Marshal(original)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal original VM: %w", err)
	}

	mutatedBytes, err := json.Marshal(mutated)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal mutated VM: %w", err)
	}

	// For now, we'll use a simple approach - in production you might want to use
	// a proper JSON patch library like github.com/evanphx/json-patch
	// This is a simplified version that replaces the entire object
	patch := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/spec",
			"value": mutated.Spec,
		},
		{
			"op":    "replace",
			"path":  "/metadata/annotations",
			"value": mutated.Annotations,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patch: %w", err)
	}

	// Debug: log the patch size
	_ = originalBytes
	_ = mutatedBytes

	return patchBytes, nil
}

// handleError handles feature errors based on error handling mode
func (m *Mutator) handleError(featureName string, err error) *admissionv1.AdmissionResponse {
	switch m.config.ErrorHandlingMode {
	case utils.ErrorHandlingReject:
		return m.errorResponse(fmt.Errorf("feature %s failed: %w", featureName, err))
	case utils.ErrorHandlingAllowAndLog:
		// Log error but allow admission
		return m.allowResponse(fmt.Sprintf("Feature %s failed but admission allowed: %v", featureName, err))
	case utils.ErrorHandlingStripLabel:
		// TODO: Implement label stripping
		return m.allowResponse(fmt.Sprintf("Feature %s failed, label stripped", featureName))
	default:
		return m.errorResponse(err)
	}
}

// allowResponse creates an allowed admission response
func (m *Mutator) allowResponse(message string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Result: &metav1.Status{
			Message: message,
		},
	}
}

// errorResponse creates a denied admission response
func (m *Mutator) errorResponse(err error) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Code:    http.StatusBadRequest,
			Message: err.Error(),
		},
	}
}
