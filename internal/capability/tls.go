// SPDX-License-Identifier: Apache-2.0
package capability

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
)

// buildTLSConfig constructs a verifying TLS client configuration for an
// https Provider. Certificate verification is never disabled. A private CA
// may be added for an internal Provider; a client certificate may be
// configured for mutual TLS.
func buildTLSConfig(provider *ProviderConfig) (*tls.Config, error) {
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: provider.TLSServerName,
	}
	if config.ServerName == "" {
		config.ServerName = provider.parsed.Hostname()
	}
	if provider.TLSCAFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pem, err := os.ReadFile(resolveProviderFile(provider, provider.TLSCAFile))
		if err != nil {
			return nil, fmt.Errorf("%w: read tls_ca_file: %v", ErrConfigInvalid, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: tls_ca_file contains no usable certificate", ErrConfigInvalid)
		}
		config.RootCAs = pool
	}
	if provider.TLSClientCertFile != "" {
		certificate, err := tls.LoadX509KeyPair(
			resolveProviderFile(provider, provider.TLSClientCertFile),
			resolveProviderFile(provider, provider.TLSClientKeyFile),
		)
		if err != nil {
			return nil, fmt.Errorf("%w: load client certificate: %v", ErrConfigInvalid, err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func resolveProviderFile(provider *ProviderConfig, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	base := provider.tlsBaseDir
	if base == "" {
		base = "."
	}
	return filepath.Join(base, path)
}
