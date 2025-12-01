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
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/userdata"
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
	client        client.Client
	config        *config.Config
	features      []features.Feature
	userdataParser *userdata.Parser
}

// NewMutator creates a new Mutator
func NewMutator(client client.Client, cfg *config.Config, featureList []features.Feature) *Mutator {
	return &Mutator{
		client:        client,
		config:        cfg,
		features:      featureList,
		userdataParser: userdata.NewParser(client),
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

	// Parse userdata for feature directives (non-fatal if fails)
	userdataFeatures, err := m.userdataParser.ParseFeatures(ctx, vm)
	if err != nil {
		logger.Error(err, "Failed to parse userdata features")
		// Non-fatal: continue with annotation-based features only
		userdataFeatures = nil
	} else if len(userdataFeatures) > 0 {
		logger.Info("Found feature directives in userdata", "features", userdataFeatures)
	}

	// Create a copy to mutate
	mutatedVM := vm.DeepCopy()

	// Merge userdata features into mutated VM's annotations (annotations take precedence)
	if len(userdataFeatures) > 0 {
		if mutatedVM.Annotations == nil {
			mutatedVM.Annotations = make(map[string]string)
		}
		for key, value := range userdataFeatures {
			if _, exists := mutatedVM.Annotations[key]; !exists {
				mutatedVM.Annotations[key] = value
				logger.Info("Applied userdata feature directive", "key", key, "value", value)
			} else {
				logger.Info("Skipping userdata feature (annotation exists)", "key", key)
			}
		}
	}

	// Log detailed feature detection information for debugging
	m.logFeatureDetection(ctx, mutatedVM)

	// Check if any features are enabled (check mutatedVM with merged userdata)
	if !m.hasEnabledFeatures(mutatedVM) {
		logger.Info("No features enabled for VM", "vm", vm.Name)
		return m.allowResponse("No features requested"), nil
	}

	// Apply features
	appliedFeatures := []string{}
	allAnnotations := make(map[string]string)

	for _, feature := range m.features {
		if !feature.IsEnabled(mutatedVM) {
			continue
		}

		logger.Info("Feature enabled", "feature", feature.Name(), "vm", vm.Name)

		// Validate
		if err := feature.Validate(ctx, mutatedVM, m.client); err != nil {
			logger.Error(err, "Feature validation failed", "feature", feature.Name())
			return m.handleError(feature.Name(), err, vm, mutatedVM), nil
		}

		// Apply
		result, err := feature.Apply(ctx, mutatedVM, m.client)
		if err != nil {
			logger.Error(err, "Feature application failed", "feature", feature.Name())
			return m.handleError(feature.Name(), err, vm, mutatedVM), nil
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

// logFeatureDetection logs detailed information about feature detection for debugging
func (m *Mutator) logFeatureDetection(ctx context.Context, vm *kubevirtv1.VirtualMachine) {
	logger := log.FromContext(ctx).V(1) // V(1) = debug level

	configMap := utils.GetConfigMap(m.config.ConfigSource, vm.GetAnnotations(), vm.GetLabels())
	if configMap == nil {
		logger.Info("VM has no configuration source data", "vm", vm.Name, "configSource", m.config.ConfigSource)
		return
	}

	logger.Info("VM configuration for feature detection",
		"vm", vm.Name,
		"configSource", m.config.ConfigSource,
		"configCount", len(configMap),
		"config", configMap)

	for _, feature := range m.features {
		enabled := feature.IsEnabled(vm)
		logger.Info("Feature detection result",
			"feature", feature.Name(),
			"enabled", enabled,
			"vm", vm.Name)
	}
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
func (m *Mutator) handleError(featureName string, err error, originalVM, mutatedVM *kubevirtv1.VirtualMachine) *admissionv1.AdmissionResponse {
	switch m.config.ErrorHandlingMode {
	case utils.ErrorHandlingReject:
		return m.errorResponse(fmt.Errorf("feature %s failed: %w", featureName, err))
	case utils.ErrorHandlingAllowAndLog:
		// Log error but allow admission
		return m.allowResponse(fmt.Sprintf("Feature %s failed but admission allowed: %v", featureName, err))
	case utils.ErrorHandlingStripLabel:
		// Strip the feature annotation and allow admission with patch
		if mutatedVM.Annotations != nil {
			// Remove the feature-specific annotation based on feature name
			annotationKey := m.getFeatureAnnotationKey(featureName)
			if annotationKey != "" {
				delete(mutatedVM.Annotations, annotationKey)
			}
		}

		// Create patch with the stripped annotation
		patch, patchErr := m.createPatch(originalVM, mutatedVM)
		if patchErr != nil {
			// If we can't create a patch, fall back to allowing without mutation
			return m.allowResponse(fmt.Sprintf("Feature %s failed, annotation strip failed: %v", featureName, patchErr))
		}

		return &admissionv1.AdmissionResponse{
			Allowed: true,
			Patch:   patch,
			PatchType: func() *admissionv1.PatchType {
				pt := admissionv1.PatchTypeJSONPatch
				return &pt
			}(),
			Result: &metav1.Status{
				Message: fmt.Sprintf("Feature %s failed, annotation %s stripped and admission allowed", featureName, m.getFeatureAnnotationKey(featureName)),
			},
		}
	default:
		return m.errorResponse(err)
	}
}

// getFeatureAnnotationKey returns the annotation key for a given feature name
func (m *Mutator) getFeatureAnnotationKey(featureName string) string {
	switch featureName {
	case utils.FeatureNestedVirt:
		return utils.AnnotationNestedVirt
	case utils.FeatureGpuDevicePlugin:
		return utils.AnnotationGpuDevicePlugin
	case utils.FeaturePciPassthrough:
		return utils.AnnotationPciPassthrough
	case utils.FeatureVBiosInjection:
		return utils.AnnotationVBiosInjection
	default:
		return ""
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
