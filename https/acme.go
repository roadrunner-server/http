package https

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/caddyserver/certmagic"
	"go.uber.org/zap"
)

type challenge string

const (
	HTTP01    challenge = "http-01"
	TLSAlpn01 challenge = "tlsalpn-01"
)

func IssueCertificates(cacheDir, email, challengeType string, domains []string, useProduction bool, altHTTPPort, altTLSAlpnPort int, log *zap.Logger) (*tls.Config, error) {
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(c certmagic.Certificate) (*certmagic.Config, error) {
			return &certmagic.Config{
				RenewalWindowRatio: 0,
				MustStaple:         false,
				OCSP:               certmagic.OCSPConfig{},
				Storage:            &certmagic.FileStorage{Path: cacheDir},
				Logger:             log,
			}, nil
		},
		OCSPCheckInterval:  0,
		RenewCheckInterval: 0,
		Capacity:           0,
	})

	cfg := certmagic.New(cache, certmagic.Config{
		RenewalWindowRatio: 0,
		MustStaple:         false,
		OCSP:               certmagic.OCSPConfig{},
		Storage:            &certmagic.FileStorage{Path: cacheDir},
		Logger:             log,
	})

	myAcme := certmagic.NewACMEIssuer(cfg, certmagic.ACMEIssuer{
		CA:                      certmagic.LetsEncryptProductionCA,
		TestCA:                  certmagic.LetsEncryptStagingCA,
		Email:                   email,
		Agreed:                  true,
		DisableHTTPChallenge:    false,
		DisableTLSALPNChallenge: false,
		ListenHost:              "0.0.0.0",
		AltHTTPPort:             altHTTPPort,
		AltTLSALPNPort:          altTLSAlpnPort,
		CertObtainTimeout:       time.Second * 240,
		PreferredChains:         certmagic.ChainPreference{},
		Logger:                  log,
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

	cfg.Issuers = append(cfg.Issuers, myAcme)

	for i := 0; i < len(domains); i++ {
		err := cfg.ObtainCertAsync(context.Background(), domains[i])
		if err != nil {
			return nil, err
		}
	}

	err := cfg.ManageSync(context.Background(), domains)
	if err != nil {
		return nil, err
	}

	return cfg.TLSConfig(), nil
}
