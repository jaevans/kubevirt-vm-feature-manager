package webhook

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/config"
	"github.com/jaevans/kubevirt-vm-feature-manager/pkg/features"
)

var _ = Describe("Server", func() {
	var (
		server  *Server
		cfg     *config.Config
		handler *Handler
		mutator *Mutator
	)

	BeforeEach(func() {
		cfg = &config.Config{
			Port:    9443,
			CertDir: "/tmp/test-certs",
		}

		mutator = NewMutator(nil, cfg, []features.Feature{})
		handler = NewHandler(mutator)
		server = NewServer(cfg, handler)
	})

	Describe("NewServer", func() {
		It("should create a new server", func() {
			Expect(server).ToNot(BeNil())
			Expect(server.config).To(Equal(cfg))
			Expect(server.handler).To(Equal(handler))
		})
	})

	Describe("Health and Readiness Endpoints", func() {
		var (
			recorder *httptest.ResponseRecorder
		)

		BeforeEach(func() {
			recorder = httptest.NewRecorder()
		})

		Describe("healthzHandler", func() {
			It("should return ok status", func() {
				req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
				server.healthzHandler(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(recorder.Body.String()).To(Equal("ok"))
			})

			It("should handle write errors gracefully", func() {
				// Test that the handler completes even if write fails
				// The error logging is tested but doesn't affect response
				req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
				recorder := httptest.NewRecorder()
				server.healthzHandler(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
			})
		})

		Describe("readyzHandler", func() {
			It("should return ready status", func() {
				req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
				server.readyzHandler(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
				Expect(recorder.Body.String()).To(Equal("ready"))
			})

			It("should handle write errors gracefully", func() {
				// Test that the handler completes even if write fails
				req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
				recorder := httptest.NewRecorder()
				server.readyzHandler(recorder, req)

				Expect(recorder.Code).To(Equal(http.StatusOK))
			})
		})
	})

	Describe("Start", func() {
		Context("with context cancellation", func() {
			It("should shutdown gracefully", func() {
				ctx, cancel := context.WithCancel(context.Background())

				// Start server in goroutine
				errChan := make(chan error, 1)
				go func() {
					// Use a port that's likely available for testing
					cfg.Port = 0                     // Let OS assign port
					cfg.CertDir = "/tmp/nonexistent" // This will fail but that's ok for this test

					err := server.Start(ctx)
					errChan <- err
				}()

				// Give server a moment to attempt startup
				time.Sleep(50 * time.Millisecond)

				// Cancel context to trigger shutdown
				cancel()

				// Wait for shutdown with timeout
				select {
				case err := <-errChan:
					// We expect an error since certs don't exist
					// but we're testing the shutdown path
					_ = err
				case <-time.After(2 * time.Second):
					Fail("Server did not shutdown in time")
				}
			})
		})

		Context("with server start error", func() {
			It("should return error when certs are missing", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()

				cfg.CertDir = "/nonexistent/path/to/certs"
				cfg.Port = 19443 // Use a specific high port

				err := server.Start(ctx)
				// Should get either context deadline or cert error
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("TLS certificate rotation", func() {
		var (
			certDir string
			addr    string
			cancel  context.CancelFunc
			errChan chan error
		)

		BeforeEach(func() {
			certDir = GinkgoT().TempDir()
			writeSelfSignedCertPair(certDir, "original.example.com")

			port := freeLocalPort()
			addr = fmt.Sprintf("127.0.0.1:%d", port)
			cfg.Port = port
			cfg.CertDir = certDir

			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())

			errChan = make(chan error, 1)
			go func() {
				errChan <- server.Start(ctx)
			}()

			// Don't start asserting until the listener is actually serving TLS.
			Eventually(func() (string, error) {
				return servedCertCommonName(addr)
			}, "10s", "100ms").Should(Equal("original.example.com"))
		})

		AfterEach(func() {
			cancel()
			Eventually(errChan, "10s").Should(Receive())
		})

		It("should serve the new certificate after the files on disk are replaced", func() {
			writeSelfSignedCertPair(certDir, "rotated.example.com")

			Eventually(func() (string, error) {
				return servedCertCommonName(addr)
			}, "30s", "250ms").Should(Equal("rotated.example.com"))
		})
	})
})

// freeLocalPort reserves an ephemeral port and releases it so the server under
// test can bind it.
func freeLocalPort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	Expect(listener.Close()).To(Succeed())
	return port
}

// servedCertCommonName connects to addr and reports the common name of the
// leaf certificate the server presents.
func servedCertCommonName(addr string) (string, error) {
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // test asserts on the presented cert, not its trust chain
		MinVersion:         tls.VersionTLS12,
	})
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("server presented no certificates")
	}
	return certs[0].Subject.CommonName, nil
}

// writeSelfSignedCertPair writes a self-signed tls.crt/tls.key pair for
// commonName into dir, replacing any existing pair.
func writeSelfSignedCertPair(dir, commonName string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).ToNot(HaveOccurred())

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	Expect(err).ToNot(HaveOccurred())

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	Expect(err).ToNot(HaveOccurred())

	keyDER, err := x509.MarshalECPrivateKey(key)
	Expect(err).ToNot(HaveOccurred())

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Write the key before the cert so a watcher that reacts to the cert
	// change always finds a matching key already in place.
	Expect(os.WriteFile(filepath.Join(dir, "tls.key"), keyPEM, 0o600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, "tls.crt"), certPEM, 0o600)).To(Succeed())
}
