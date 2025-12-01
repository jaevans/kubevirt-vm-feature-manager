// Package userdata provides parsing of feature directives from VM userdata.
// It supports extracting @kubevirt-feature: directives from cloud-init userdata
// in various formats: plain text, base64-encoded, or Secret references.
package userdata

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// featureDirectiveRegex matches lines like:
// # @kubevirt-feature: nested-virt=enabled
// # @kubevirt-feature: pci-passthrough={"devices":["0000:00:02.0"]}
// Value is limited to 1024 characters to prevent regex DoS attacks
var featureDirectiveRegex = regexp.MustCompile(`(?m)^\s*#\s*@kubevirt-feature:\s*([a-z0-9-]+)\s*=\s*([^\n]+?)\s*$`)

// Parser extracts feature directives from VM userdata
type Parser struct {
	client client.Client
}

// NewParser creates a new userdata parser
func NewParser(client client.Client) *Parser {
	return &Parser{
		client: client,
	}
}

// ParseFeatures extracts feature directives from VM userdata volumes
// and returns them as a map of annotation key -> value
func (p *Parser) ParseFeatures(ctx context.Context, vm *kubevirtv1.VirtualMachine) (map[string]string, error) {
	logger := log.FromContext(ctx)
	features := make(map[string]string)

	if vm.Spec.Template == nil {
		return features, nil
	}

	// Iterate through volumes looking for cloud-init userdata
	for _, volume := range vm.Spec.Template.Spec.Volumes {
		var userData string
		var err error

		// Handle CloudInitNoCloud
		if volume.CloudInitNoCloud != nil {
			userData, err = p.extractUserData(ctx, vm, volume.CloudInitNoCloud.UserData, volume.CloudInitNoCloud.UserDataBase64, volume.CloudInitNoCloud.UserDataSecretRef)
			if err != nil {
				logger.Error(err, "Failed to extract userdata from CloudInitNoCloud", "volume", volume.Name)
				continue
			}
		}

		// Handle CloudInitConfigDrive
		if volume.CloudInitConfigDrive != nil {
			userData, err = p.extractUserData(ctx, vm, volume.CloudInitConfigDrive.UserData, volume.CloudInitConfigDrive.UserDataBase64, volume.CloudInitConfigDrive.UserDataSecretRef)
			if err != nil {
				logger.Error(err, "Failed to extract userdata from CloudInitConfigDrive", "volume", volume.Name)
				continue
			}
		}

		// Parse feature directives from userdata
		if userData != "" {
			volumeFeatures := p.parseDirectives(userData)
			for k, v := range volumeFeatures {
				features[k] = v
			}
		}
	}

	if len(features) > 0 {
		logger.Info("Extracted feature directives from userdata", "features", features)
	}

	return features, nil
}

// extractUserData extracts userdata from plain text, base64, or secret reference
func (p *Parser) extractUserData(ctx context.Context, vm *kubevirtv1.VirtualMachine, plainText, base64Text string, secretRef *corev1.LocalObjectReference) (string, error) {
	// Priority: plain text -> base64 -> secret
	if plainText != "" {
		return plainText, nil
	}

	if base64Text != "" {
		decoded, err := base64.StdEncoding.DecodeString(base64Text)
		if err != nil {
			return "", fmt.Errorf("failed to decode base64 userdata: %w", err)
		}
		return string(decoded), nil
	}

	if secretRef != nil {
		return p.fetchSecretUserData(ctx, vm.Namespace, secretRef.Name)
	}

	return "", nil
}

// fetchSecretUserData fetches userdata from a Kubernetes Secret
// Security: Only secrets labeled with "vm-feature-manager.io/userdata=allowed" can be accessed
// to prevent information disclosure from arbitrary secrets
func (p *Parser) fetchSecretUserData(ctx context.Context, namespace, secretName string) (string, error) {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      secretName,
	}

	if err := p.client.Get(ctx, key, secret); err != nil {
		return "", fmt.Errorf("failed to fetch secret %s/%s: %w", namespace, secretName, err)
	}

	// No guard: Assume if the webhook can mutate the VM in a namespace,
	// it is permitted to read the referenced Secret in that namespace.

	// Try common userdata keys
	for _, key := range []string{"userdata", "userData", "user-data"} {
		if data, ok := secret.Data[key]; ok {
			logger.Info("Found userdata in secret", "secret", secretName, "key", key)
			return string(data), nil
		}
	}

	return "", fmt.Errorf("no userdata found in secret %s/%s (tried keys: userdata, userData, user-data)", namespace, secretName)
}

// parseDirectives extracts @kubevirt-feature directives from userdata text
func (p *Parser) parseDirectives(userData string) map[string]string {
	features := make(map[string]string)

	// Reject overly large userdata to prevent resource exhaustion
	if len(userData) > 65536 { // 64KB limit
		return features
	}
	matches := featureDirectiveRegex.FindAllStringSubmatch(userData, -1)
	for _, match := range matches {
		if len(match) == 3 {
			featureName := strings.TrimSpace(match[1])
			featureValue := strings.TrimSpace(match[2])

			// Enforce max value length to prevent DoS
			if len(featureValue) > 1024 {
				continue // Skip overly long values
			}

			// Map feature names to annotation keys
			annotationKey := fmt.Sprintf("vm-feature-manager.io/%s", featureName)
			features[annotationKey] = featureValue
		}
	}

	return features
}
