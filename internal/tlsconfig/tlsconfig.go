// Package tlsconfig builds *tls.Config instances from user-supplied
// verification settings (custom CA, insecure mode, certificate
// pinning). It is shared by the WebSocket transport and the HTTP
// login client so both paths enforce identical TLS policy.
package tlsconfig

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds TLS verification settings for a server connection.
type Config struct {
	ServerName         string // TLS SNI hostname (for CDN edge IP connections)
	CACertPEM          string // inline CA certificate PEM content
	CACertPath         string // path to a CA certificate file
	InsecureSkipVerify bool   // skip chain verification entirely
	PinnedCertHash     string // SHA-256 fingerprint of the leaf cert (hex, optional "sha256:" prefix)
}

// Build creates a *tls.Config from the given settings.
//
// Verification behaviour matrix:
//
//	Pin set | Insecure | Behaviour
//	--------+----------+-------------------------------------------
//	  no    |   no     | Normal chain + hostname verification (Go default)
//	  no    |   yes    | All verification skipped
//	  yes   |   no     | Normal chain verification, then pin check
//	  yes   |   yes    | Chain skipped, pin check only
func Build(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		ServerName: cfg.ServerName,
	}

	if cfg.CACertPEM != "" || cfg.CACertPath != "" {
		pool, err := buildCertPool(cfg.CACertPEM, cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("load CA cert: %w", err)
		}
		tlsCfg.RootCAs = pool
	}

	var pin []byte
	if cfg.PinnedCertHash != "" {
		var err error
		pin, err = decodePin(cfg.PinnedCertHash)
		if err != nil {
			return nil, fmt.Errorf("invalid pinned cert hash: %w", err)
		}
	}

	if pin != nil {
		if cfg.InsecureSkipVerify {
			tlsCfg.InsecureSkipVerify = true
		}
		tlsCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPin(rawCerts, pin)
		}
	} else if cfg.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true
	}

	return tlsCfg, nil
}

// PinMismatchError indicates the server leaf certificate does not
// match the pinned SHA-256 fingerprint.
type PinMismatchError struct {
	Expected []byte
	Got      []byte
}

func (e *PinMismatchError) Error() string {
	return fmt.Sprintf("certificate pin mismatch: expected %x, got %x", e.Expected, e.Got)
}

// IsTLSError reports whether err represents a TLS certificate
// verification failure that will not resolve on retry.
func IsTLSError(err error) bool {
	var certInvalid *x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return true
	}
	var hostErr *x509.HostnameError
	if errors.As(err, &hostErr) {
		return true
	}
	var unknownAuth *x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return true
	}
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return true
	}
	var pinErr *PinMismatchError
	if errors.As(err, &pinErr) {
		return true
	}
	return false
}

func buildCertPool(pemData, path string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()

	if pemData != "" {
		if !pool.AppendCertsFromPEM([]byte(pemData)) {
			return nil, errors.New("failed to parse inline CA certificate PEM")
		}
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read CA cert file %s: %w", path, err)
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("failed to parse CA certificate file")
		}
	}

	return pool, nil
}

func decodePin(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "sha256:") {
		s = s[len("sha256:"):]
	}
	pin, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(pin) != sha256.Size {
		return nil, fmt.Errorf("expected %d bytes, got %d", sha256.Size, len(pin))
	}
	return pin, nil
}

func verifyPin(rawCerts [][]byte, pin []byte) error {
	if len(rawCerts) == 0 {
		return errors.New("no server certificate provided")
	}
	h := sha256.Sum256(rawCerts[0])
	if !bytes.Equal(h[:], pin) {
		return &PinMismatchError{Expected: pin, Got: h[:]}
	}
	return nil
}
