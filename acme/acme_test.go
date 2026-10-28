package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
)

func TestNewConfigIssuer(t *testing.T) {
	tests := []struct {
		name            string
		production      bool
		challenge       string
		wantCA          string
		wantDisableHTTP bool
		wantDisableTLS  bool
	}{
		{
			name:            "staging with TLS ALPN",
			challenge:       "tlsalpn-01",
			wantCA:          certmagic.LetsEncryptStagingCA,
			wantDisableHTTP: true,
		},
		{
			name:           "staging with HTTP",
			challenge:      "http-01",
			wantCA:         certmagic.LetsEncryptStagingCA,
			wantDisableTLS: true,
		},
		{
			name:            "production with TLS ALPN",
			production:      true,
			challenge:       "tlsalpn-01",
			wantCA:          certmagic.LetsEncryptProductionCA,
			wantDisableHTTP: true,
		},
		{
			name:           "production with HTTP",
			production:     true,
			challenge:      "http-01",
			wantCA:         certmagic.LetsEncryptProductionCA,
			wantDisableTLS: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, cache := newConfig(t.TempDir(), "hostmaster@example.org", tt.challenge, tt.production, 8080, 8443)
			t.Cleanup(cache.Stop)
			if len(cfg.Issuers) != 1 {
				t.Fatalf("issuer count = %d, want 1", len(cfg.Issuers))
			}
			issuer, ok := cfg.Issuers[0].(*certmagic.ACMEIssuer)
			if !ok {
				t.Fatalf("issuer type = %T, want *certmagic.ACMEIssuer", cfg.Issuers[0])
			}
			if issuer.CA != tt.wantCA {
				t.Errorf("CA = %q, want %q", issuer.CA, tt.wantCA)
			}
			if issuer.Email != "hostmaster@example.org" {
				t.Errorf("email = %q, want hostmaster@example.org", issuer.Email)
			}
			if issuer.DisableHTTPChallenge != tt.wantDisableHTTP || issuer.DisableTLSALPNChallenge != tt.wantDisableTLS {
				t.Errorf("challenge flags = (%t, %t), want (%t, %t)", issuer.DisableHTTPChallenge, issuer.DisableTLSALPNChallenge, tt.wantDisableHTTP, tt.wantDisableTLS)
			}
			if issuer.AltHTTPPort != 8080 || issuer.AltTLSALPNPort != 8443 {
				t.Errorf("alternate ports = (%d, %d), want (8080, 8443)", issuer.AltHTTPPort, issuer.AltTLSALPNPort)
			}
		})
	}
}

func TestCacheReloadsRenewedCertificate(t *testing.T) {
	cfg, cache := newConfig(t.TempDir(), "hostmaster@example.org", "tlsalpn-01", false, 8080, 8443)
	t.Cleanup(cache.Stop)
	cfg.DisableARI = true

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	const domain = "renewal.example.org"
	issuerKey := cfg.Issuers[0].IssuerKey()
	if err := cfg.Storage.Store(t.Context(), certmagic.StorageKeys.SitePrivateKey(issuerKey, domain), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Storage.Store(t.Context(), certmagic.StorageKeys.SiteMeta(issuerKey, domain), []byte(`{"sans":["renewal.example.org"]}`)); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	certificates := []struct {
		name      string
		serial    int64
		notBefore time.Time
		notAfter  time.Time
		cache     bool
	}{
		{name: "cached certificate due for renewal", serial: 1, notBefore: now.Add(-89 * 24 * time.Hour), notAfter: now.Add(24 * time.Hour), cache: true},
		{name: "renewed certificate in storage", serial: 2, notBefore: now.Add(-time.Hour), notAfter: now.Add(90 * 24 * time.Hour)},
	}
	for _, certificate := range certificates {
		template := &x509.Certificate{
			SerialNumber: big.NewInt(certificate.serial),
			DNSNames:     []string{domain},
			NotBefore:    certificate.notBefore,
			NotAfter:     certificate.notAfter,
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
		if err != nil {
			t.Fatalf("create %s: %v", certificate.name, err)
		}
		if err := cfg.Storage.Store(t.Context(), certmagic.StorageKeys.SiteCert(issuerKey, domain), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
			t.Fatalf("store %s: %v", certificate.name, err)
		}
		if certificate.cache {
			if _, err := cfg.CacheManagedCertificate(t.Context(), domain); err != nil {
				t.Fatalf("cache %s: %v", certificate.name, err)
			}
		}
	}
	if err := cache.RenewManagedCertificates(t.Context()); err != nil {
		t.Fatal(err)
	}
	cached := cache.AllMatchingCertificates(domain)
	if len(cached) != 1 {
		t.Fatalf("cached certificate count = %d, want 1", len(cached))
	}
	if cached[0].Leaf.SerialNumber.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("cached certificate serial = %s, want 2", cached[0].Leaf.SerialNumber)
	}
}
