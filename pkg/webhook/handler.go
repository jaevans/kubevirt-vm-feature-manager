// Package webhook implements the HTTP server and admission webhook handlers
// for the KubeVirt VM Feature Manager. It processes admission requests,
// applies feature mutations to VirtualMachine objects, and returns JSON patches.
package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Handler wraps the mutator and handles HTTP requests
type Handler struct {
	mutator *Mutator
}

// NewHandler creates a new webhook handler
func NewHandler(mutator *Mutator) *Handler {
	return &Handler{
		mutator: mutator,
	}
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(err, "Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer func() {
		if closeErr := r.Body.Close(); closeErr != nil {
			logger.Error(closeErr, "Failed to close request body")
		}
	}()

	// Decode admission review
	admissionReview := &admissionv1.AdmissionReview{}
	if err := json.Unmarshal(body, admissionReview); err != nil {
		logger.Error(err, "Failed to unmarshal admission review")
		http.Error(w, "Failed to unmarshal admission review", http.StatusBadRequest)
		return
	}

	if admissionReview.Request == nil {
		logger.Error(nil, "Admission review request is nil")
		http.Error(w, "Admission review request is nil", http.StatusBadRequest)
		return
	}

	// Handle the admission request
	admissionResponse, err := h.mutator.Handle(ctx, admissionReview.Request)
	if err != nil {
		logger.Error(err, "Failed to handle admission request")
		admissionResponse = &admissionv1.AdmissionResponse{
			UID:     admissionReview.Request.UID,
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusInternalServerError,
				Message: fmt.Sprintf("Internal error: %v", err),
			},
		}
	}

	// Ensure UID is always set (in case mutator helpers didn't set it)
	if admissionResponse.UID == "" {
		admissionResponse.UID = admissionReview.Request.UID
	}

	// Construct response
	responseReview := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	// Marshal response
	responseBytes, err := json.Marshal(responseReview)
	if err != nil {
		logger.Error(err, "Failed to marshal response")
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(responseBytes); err != nil {
		logger.Error(err, "Failed to write response")
	}
}
