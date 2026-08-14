package helpers

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	clientCertPath = "test-certs/localhost+2-client.pem"
	clientKeyPath  = "test-certs/localhost+2-client-key.pem"
)

// MTLSClient returns a client presenting the test client certificate, for the
// servers configured with client_auth_type.
func MTLSClient(t *testing.T) *http.Client {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	require.NoError(t, err)

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:   tls.VersionTLS12,
				Certificates: []tls.Certificate{cert},
			},
		},
	}
}
