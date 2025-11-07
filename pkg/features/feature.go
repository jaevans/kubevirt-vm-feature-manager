// Package features provides the feature implementation framework for VM mutations.
package features

import (
	"context"

	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Feature represents a VM feature that can be applied via mutation
type Feature interface {
	// Name returns the feature name for logging and tracking
	Name() string

	// IsEnabled checks if the feature is requested via annotations
	IsEnabled(vm *kubevirtv1.VirtualMachine) bool

	// Apply modifies the VM spec to enable the feature
	// Returns a MutationResult with tracking information
	Apply(ctx context.Context, vm *kubevirtv1.VirtualMachine, client client.Client) (*MutationResult, error)

	// Validate performs basic validation before applying
	// This is lightweight validation (format checks, resource existence)
	// Heavy validation (host capabilities) belongs in the validating webhook
	Validate(ctx context.Context, vm *kubevirtv1.VirtualMachine, client client.Client) error
}

// MutationResult contains information about what was mutated
type MutationResult struct {
	// Applied indicates if the feature was successfully applied
	Applied bool

	// Annotations are tracking annotations to add to the VM
	Annotations map[string]string

	// Messages are informational messages about the mutation
	Messages []string
}

// NewMutationResult creates a new MutationResult
func NewMutationResult() *MutationResult {
	return &MutationResult{
		Applied:     false,
		Annotations: make(map[string]string),
		Messages:    []string{},
	}
}

// AddAnnotation adds a tracking annotation
func (r *MutationResult) AddAnnotation(key, value string) {
	r.Annotations[key] = value
}

// AddMessage adds an informational message
func (r *MutationResult) AddMessage(msg string) {
	r.Messages = append(r.Messages, msg)
}
