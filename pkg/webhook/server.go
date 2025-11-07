package webhook

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

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

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
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

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		certFile := fmt.Sprintf("%s/tls.crt", s.config.CertDir)
		keyFile := fmt.Sprintf("%s/tls.key", s.config.CertDir)

		if err := s.server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
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
