package webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
)

// Server represents the webhook HTTP server
type Server struct {
	config  *config.Config
	handler *Handler
	server  *http.Server
}

// NewServer creates a new webhook server
func NewServer(cfg *config.Config, handler *Handler) *Server {
	return &Server{
		config:  cfg,
		handler: handler,
	}
}

// Start starts the webhook server
func (s *Server) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)

	mux := http.NewServeMux()
	mux.Handle("/mutate", s.handler)
	mux.HandleFunc("/healthz", s.healthzHandler)
	mux.HandleFunc("/readyz", s.readyzHandler)

	certFile := filepath.Join(s.config.CertDir, "tls.crt")
	keyFile := filepath.Join(s.config.CertDir, "tls.key")

	// Watch the keypair on disk rather than handing the paths to
	// ListenAndServeTLS, which parses them once and caches the result for the
	// lifetime of the process. The certificate is mounted from a Secret that
	// cert-manager rotates well before expiry, so a cached keypair leaves the
	// webhook serving an expired certificate until someone restarts the pod.
	// Because the webhook has failurePolicy: Fail, that takes down every
	// VirtualMachine create in the cluster.
	certWatcher, err := certwatcher.New(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS keypair from %s: %w", s.config.CertDir, err)
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: certWatcher.GetCertificate,
	}

	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      mux,
		TLSConfig:    tlsConfig,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	logger.Info("Starting webhook server",
		"port", s.config.Port,
		"certDir", s.config.CertDir)

	errChan := make(chan error, 2)

	// Reload the keypair on change. certwatcher combines fsnotify events with
	// periodic polling, so it survives the atomic symlink swap kubelet uses
	// when updating a mounted Secret.
	go func() {
		if err := certWatcher.Start(ctx); err != nil {
			errChan <- fmt.Errorf("certificate watcher failed: %w", err)
		}
	}()

	// Start server in a goroutine
	go func() {
		// The keypair is supplied by tlsConfig.GetCertificate, so the file
		// arguments are intentionally empty.
		if err := s.server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		logger.Info("Shutting down webhook server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	case err := <-errChan:
		return err
	}
}

// healthzHandler handles health check requests
func (s *Server) healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		// Log error but don't fail - response status already sent
		log.Log.Error(err, "Failed to write health check response")
	}
}

// readyzHandler handles readiness check requests
func (s *Server) readyzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ready")); err != nil {
		// Log error but don't fail - response status already sent
		log.Log.Error(err, "Failed to write readiness check response")
	}
}
