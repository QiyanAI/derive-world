// SPDX-License-Identifier: Apache-2.0

// Command capability-provider is the independent, stdlib-only reference
// Provider for content.structure. It never imports the Derive World main
// module, never connects to PostgreSQL, and never receives user cookies, PATs,
// private AI context, or database credentials.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const (
	fixedProviderID    = "reference.content-structure"
	fixedContract      = "1.0"
	fixedCapabilityID  = "content.structure"
	defaultListenAddr  = ":8081"
	defaultMaxBody     = 65536
	defaultReplayCache = 4096
)

type config struct {
	secret                 []byte
	keyID                  string
	previousSecret         []byte
	previousKeyID          string
	previousUntil          time.Time
	requireResponseSigning bool
	tlsCertFile            string
	tlsKeyFile             string
	tlsClientCAFile        string
	requireClientCert      bool
	listenAddr             string
	providerID             string
	contract               string
	maxBodyBytes           int64
	replaySize             int
}

func loadConfig() config {
	secret := []byte(os.Getenv("CAPABILITY_HMAC_SECRET"))
	if len(secret) < 32 {
		log.Fatal("CAPABILITY_HMAC_SECRET must be at least 32 bytes")
	}
	keyID := os.Getenv("CAPABILITY_HMAC_KEY_ID")
	if keyID == "" {
		keyID = "k1"
	}
	if !validProviderKeyID(keyID) {
		log.Fatal("CAPABILITY_HMAC_KEY_ID must be 1..16 lowercase letters, digits, or hyphens")
	}
	previousSecret := []byte(os.Getenv("CAPABILITY_HMAC_PREVIOUS_SECRET"))
	previousKeyID := os.Getenv("CAPABILITY_HMAC_PREVIOUS_KEY_ID")
	previousUntilRaw := os.Getenv("CAPABILITY_HMAC_PREVIOUS_UNTIL")
	var previousUntil time.Time
	if len(previousSecret) > 0 || previousKeyID != "" || previousUntilRaw != "" {
		if len(previousSecret) < 32 {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_SECRET must be at least 32 bytes when rotation is configured")
		}
		if previousKeyID == "" || previousUntilRaw == "" {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_KEY_ID and CAPABILITY_HMAC_PREVIOUS_UNTIL are required with a previous secret")
		}
		if !validProviderKeyID(previousKeyID) || previousKeyID == keyID {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_KEY_ID must be valid and differ from the current key id")
		}
		if string(previousSecret) == string(secret) {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_SECRET must differ from the current secret")
		}
		parsed, err := time.Parse(time.RFC3339, previousUntilRaw)
		if err != nil {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_UNTIL must be an RFC3339 timestamp")
		}
		if !parsed.After(time.Now()) {
			log.Fatal("CAPABILITY_HMAC_PREVIOUS_UNTIL must be in the future at startup; remove the previous key instead")
		}
		previousUntil = parsed
	}
	requireResponseSigning := true
	if raw := os.Getenv("REQUIRE_RESPONSE_SIGNING"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			log.Fatal("REQUIRE_RESPONSE_SIGNING must be true or false")
		}
		requireResponseSigning = parsed
	}
	tlsCertFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	if (tlsCertFile == "") != (tlsKeyFile == "") {
		log.Fatal("TLS_CERT_FILE and TLS_KEY_FILE must be set together")
	}
	if tlsCertFile != "" {
		if _, err := tls.LoadX509KeyPair(tlsCertFile, tlsKeyFile); err != nil {
			log.Fatalf("TLS certificate could not be loaded: %v", err)
		}
	}
	tlsClientCAFile := os.Getenv("TLS_CLIENT_CA_FILE")
	requireClientCert := false
	if raw := os.Getenv("TLS_REQUIRE_CLIENT_CERT"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			log.Fatal("TLS_REQUIRE_CLIENT_CERT must be true or false")
		}
		requireClientCert = parsed
	}
	if requireClientCert && tlsClientCAFile == "" {
		log.Fatal("TLS_CLIENT_CA_FILE is required when TLS_REQUIRE_CLIENT_CERT=true")
	}
	if requireClientCert && tlsCertFile == "" {
		log.Fatal("TLS_REQUIRE_CLIENT_CERT requires TLS_CERT_FILE and TLS_KEY_FILE")
	}
	providerID := os.Getenv("PROVIDER_ID")
	if providerID == "" {
		providerID = fixedProviderID
	}
	if providerID != fixedProviderID {
		log.Fatal("PROVIDER_ID must be reference.content-structure")
	}
	contract := os.Getenv("CONTRACT_VERSION")
	if contract == "" {
		contract = fixedContract
	}
	if contract != fixedContract {
		log.Fatal("CONTRACT_VERSION must be 1.0")
	}
	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	maxBody := int64(defaultMaxBody)
	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || parsed > defaultMaxBody {
			log.Fatal("MAX_BODY_BYTES must be 1..65536")
		}
		maxBody = parsed
	}
	replaySize := defaultReplayCache
	if raw := os.Getenv("REPLAY_CACHE_SIZE"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65536 {
			log.Fatal("REPLAY_CACHE_SIZE must be 1..65536")
		}
		replaySize = parsed
	}
	return config{
		secret: secret, keyID: keyID,
		previousSecret: previousSecret, previousKeyID: previousKeyID,
		previousUntil:          previousUntil,
		requireResponseSigning: requireResponseSigning,
		tlsCertFile:            tlsCertFile, tlsKeyFile: tlsKeyFile,
		tlsClientCAFile: tlsClientCAFile, requireClientCert: requireClientCert,
		listenAddr: listenAddr, providerID: providerID,
		contract: contract, maxBodyBytes: maxBody, replaySize: replaySize,
	}
}

// validProviderKeyID mirrors the Broker's key-id rule.
func validProviderKeyID(value string) bool {
	if len(value) < 1 || len(value) > 16 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if char == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}

func main() {
	cfg := loadConfig()
	handler := newHandler(cfg)
	server := &http.Server{
		Addr:              cfg.listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	if cfg.tlsCertFile != "" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.requireClientCert {
			pool := x509.NewCertPool()
			pem, err := os.ReadFile(cfg.tlsClientCAFile)
			if err != nil {
				log.Fatalf("TLS_CLIENT_CA_FILE could not be read: %v", err)
			}
			if !pool.AppendCertsFromPEM(pem) {
				log.Fatal("TLS_CLIENT_CA_FILE contains no usable certificate")
			}
			tlsConfig.ClientCAs = pool
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
		server.TLSConfig = tlsConfig
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("capability_provider_started provider_id=%s listen=%s tls=%v response_signing_required=%v",
		cfg.providerID, cfg.listenAddr, cfg.tlsCertFile != "", cfg.requireResponseSigning)
	var serveErr error
	if cfg.tlsCertFile != "" {
		serveErr = server.ListenAndServeTLS(cfg.tlsCertFile, cfg.tlsKeyFile)
	} else {
		serveErr = server.ListenAndServe()
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
}
