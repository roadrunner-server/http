package middleware

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

func TLSInfo(next http.Handler, log *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("TLSInfo", zap.String("host", r.Host), zap.String("path", r.URL.Path))

		if r.TLS == nil {
			next.ServeHTTP(w, r)
			return
		}

		info := tlsInfo(r)
		if info == nil {
			next.ServeHTTP(w, r)
			return
		}

		tlsinfo, err := json.Marshal(info)
		if err != nil {
			log.Warn("failed to parse TLS info", zap.Error(err))
		}

		r.Header.Add("RR_TLS_INFO", string(tlsinfo))
		next.ServeHTTP(w, r)
		r.Header.Del("RR_TLS_INFO")
	})
}

func tlsInfo(r *http.Request) map[string]any {
	info := make(map[string]any)

	info["version"] = r.TLS.Version
	info["cipher_suite"] = r.TLS.CipherSuite
	info["server_name"] = r.TLS.ServerName

	// Connection state
	info["handshake_complete"] = r.TLS.HandshakeComplete
	info["did_resume"] = r.TLS.DidResume
	info["negotiated_protocol"] = r.TLS.NegotiatedProtocol

	// Certificate information
	if len(r.TLS.PeerCertificates) > 0 {
		var certs []map[string]any
		for _, cert := range r.TLS.PeerCertificates {
			certInfo := map[string]any{
				"subject":              cert.Subject.String(),
				"issuer":               cert.Issuer.String(),
				"serial":               cert.SerialNumber.String(),
				"not_before":           cert.NotBefore,
				"not_after":            cert.NotAfter,
				"dns_names":            cert.DNSNames,
				"ip_addresses":         cert.IPAddresses,
				"signature_algorithm":  cert.SignatureAlgorithm.String(),
				"public_key_algorithm": cert.PublicKeyAlgorithm.String(),
				"is_ca":                cert.IsCA,
			}
			certs = append(certs, certInfo)
		}
		info["peer_certificates"] = certs
	}

	// Verified chains
	if len(r.TLS.VerifiedChains) > 0 {
		var chains [][]map[string]any
		for _, chain := range r.TLS.VerifiedChains {
			var chainInfo []map[string]any
			for _, cert := range chain {
				certInfo := map[string]any{
					"subject": cert.Subject.String(),
					"issuer":  cert.Issuer.String(),
					"serial":  cert.SerialNumber.String(),
				}
				chainInfo = append(chainInfo, certInfo)
			}
			chains = append(chains, chainInfo)
		}
		info["verified_chains"] = chains
	}

	// OCSP Response
	if len(r.TLS.OCSPResponse) > 0 {
		info["ocsp_response_length"] = len(r.TLS.OCSPResponse)
	}

	// TLS Unique
	if len(r.TLS.TLSUnique) > 0 {
		info["tls_unique_length"] = len(r.TLS.TLSUnique)
	}

	return info
}
