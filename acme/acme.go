package acme

import (
	"context"
	"crypto/tls"
	"log/slog"
	"time"

	"github.com/caddyserver/certmagic"
)

type challenge string

const (
	HTTP01    challenge = "http-01"
	TLSAlpn01 challenge = "tlsalpn-01"
)

func IssueCertificates(cacheDir, email, challengeType string, domains []string, useProduction bool, altHTTPPort, altTLSAlpnPort int, log *slog.Logger) (*tls.Config, error) {
	cfg, cache := newConfig(cacheDir, email, challengeType, useProduction, altHTTPPort, altTLSAlpnPort)

	for _, domain := range domains {
		err := cfg.ObtainCertAsync(context.Background(), domain)
		if err != nil {
			cache.Stop()
			return nil, err
		}
	}

	err := cfg.ManageSync(context.Background(), domains)
	if err != nil {
		cache.Stop()
		return nil, err
	}

	return cfg.TLSConfig(), nil
}

func newConfig(cacheDir, email, challengeType string, useProduction bool, altHTTPPort, altTLSAlpnPort int) (*certmagic.Config, *certmagic.Cache) {
	var cfg *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(_ certmagic.Certificate) (*certmagic.Config, error) {
			return cfg, nil
		},
	})

	cfg = certmagic.New(cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: cacheDir},
	})

	myAcme := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
		CA:                certmagic.LetsEncryptProductionCA,
		TestCA:            certmagic.LetsEncryptStagingCA,
		Email:             email,
		Agreed:            true,
		ListenHost:        "0.0.0.0",
		AltHTTPPort:       altHTTPPort,
		AltTLSALPNPort:    altTLSAlpnPort,
		CertObtainTimeout: time.Second * 240,
	})

	if !useProduction {
		myAcme.CA = certmagic.LetsEncryptStagingCA
	}

	switch challenge(challengeType) {
	case HTTP01:
		myAcme.DisableTLSALPNChallenge = true
	case TLSAlpn01:
		myAcme.DisableHTTPChallenge = true
	default:
		// default - http
		myAcme.DisableTLSALPNChallenge = true
	}

	cfg.Issuers = []certmagic.Issuer{myAcme}

	return cfg, cache
}
