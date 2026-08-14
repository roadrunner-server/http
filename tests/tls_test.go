package tests

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"testing"

	"tests/helpers"

	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/send/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
)

const (
	// testServerCert is the leaf the ssl configs serve; the client keypair is the
	// one helpers.MTLSClient presents.
	testServerCert = "test-certs/localhost+2.pem"
	testClientCert = "test-certs/localhost+2-client.pem"
	testClientKey  = "test-certs/localhost+2-client-key.pem"
)

// skipWithoutTrustedCA skips a test that needs a working TLS handshake against
// the test certificates. Those come from mkcert and the clients here verify
// them, so without `mkcert -install` every handshake fails. CI installs the CA
// before running the suite, a developer machine usually has not.
func skipWithoutTrustedCA(t *testing.T) {
	t.Helper()

	if !testCAInstalled() {
		t.Skip("mkcert CA is not in the system trust store, run `mkcert -install` to enable the TLS tests")
	}
}

// testCAInstalled reports whether the certificate the test servers present
// chains to a CA in the system trust store.
func testCAInstalled() bool {
	raw, err := os.ReadFile(testServerCert)
	if err != nil {
		return false
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return false
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}

	// no DNSName: the question is whether the chain is trusted, not who it is for
	_, err = cert.Verify(x509.VerifyOptions{})

	return err == nil
}

// The same worker answers over plain http, over tls and over fastcgi.
func TestSSL(t *testing.T) {
	skipWithoutTrustedCA(t)

	helpers.Start(t, "configs/.rr-ssl.yaml", []any{
		&send.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:8893"))

	client := helpers.MTLSClient(t)

	t.Run("overTLS", func(t *testing.T) {
		res := clientGet(t, client, "https://127.0.0.1:8893?hello=world")
		require.NotNil(t, res.TLS)
		require.Equal(t, 201, res.StatusCode)
		require.Equal(t, "WORLD", res.Body)
	})

	// redirect is off, so the plain address serves the worker instead of bouncing to tls
	t.Run("plainNotRedirected", func(t *testing.T) {
		helpers.WaitListener(t, "tcp", "127.0.0.1:8085")

		res := clientGet(t, client, "http://127.0.0.1:8085?hello=world")
		require.Nil(t, res.TLS)
		require.Equal(t, 201, res.StatusCode)
		require.Equal(t, "WORLD", res.Body)
	})

	t.Run("overFCGI", func(t *testing.T) {
		code, body := fcgiGet(t, "tcp", "127.0.0.1:16920", "http://site.local/?hello=world")
		require.Equal(t, 201, code)
		require.Equal(t, "WORLD", body)
	})
}

// With redirect on, a plain request lands on the tls address.
func TestSSLRedirect(t *testing.T) {
	skipWithoutTrustedCA(t)

	helpers.Start(t, "configs/.rr-ssl-redirect.yaml", []any{
		&send.Plugin{},
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:8087"))

	helpers.WaitListener(t, "tcp", "127.0.0.1:8895")

	res := clientGet(t, helpers.MTLSClient(t), "http://127.0.0.1:8087?hello=world")
	require.NotNil(t, res.TLS)
	require.Equal(t, 201, res.StatusCode)
	require.Equal(t, "WORLD", res.Body)
}

// A worker pushing over a pipe relay gets no Http2-Release header back.
func TestSSLPushPipes(t *testing.T) {
	skipWithoutTrustedCA(t)

	helpers.Start(t, "configs/.rr-ssl-push.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:8894"))

	res := clientGet(t, helpers.MTLSClient(t), "https://127.0.0.1:8894?hello=world")
	require.NotNil(t, res.TLS)
	require.Empty(t, res.Header.Get("Http2-Release"))
	require.Equal(t, 201, res.StatusCode)
	require.Equal(t, "WORLD", res.Body)
}

// A config with an ssl section and no plain address serves tls only.
func TestSSLNoHTTP(t *testing.T) {
	skipWithoutTrustedCA(t)

	helpers.Start(t, "configs/.rr-ssl-no-http.yaml", []any{
		&server.Plugin{},
		&httpPlugin.Plugin{},
	}, helpers.WithTCPProbe("127.0.0.1:4455"))

	res := clientGet(t, helpers.MTLSClient(t), "https://127.0.0.1:4455?hello=world")
	require.Equal(t, 201, res.StatusCode)
	require.Equal(t, "WORLD", res.Body)
}

// Every client_auth_type accepts the client certificate; only the strictest one
// rejects a client that presents none.
func TestMTLS(t *testing.T) {
	skipWithoutTrustedCA(t)

	for _, tt := range []struct {
		name       string
		cfg        string
		addr       string
		clientCert bool
	}{
		{name: "requireAndVerifyClientCert", cfg: "configs/.rr-mtls1.yaml", addr: "127.0.0.1:8895", clientCert: true},
		{name: "verifyClientCertIfGiven", cfg: "configs/.rr-mtls2.yaml", addr: "127.0.0.1:8896", clientCert: true},
		{name: "requireAnyClientCert", cfg: "configs/.rr-mtls3.yaml", addr: "127.0.0.1:8897", clientCert: true},
		{name: "requestClientCert", cfg: "configs/.rr-mtls4.yaml", addr: "127.0.0.1:8898", clientCert: true},
		{name: "requireAndVerifyClientCertWithoutOne", cfg: "configs/.rr-mtls1.yaml", addr: "127.0.0.1:8895", clientCert: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			helpers.Start(t, tt.cfg, []any{
				&server.Plugin{},
				&httpPlugin.Plugin{},
			}, helpers.WithTCPProbe(tt.addr))

			url := "https://" + tt.addr + "?hello=world"

			if !tt.clientCert {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
				require.NoError(t, err)

				//nolint:bodyclose // the handshake fails, there is no body
				_, err = plainTLSClient().Do(req)
				require.Error(t, err)

				return
			}

			res := clientGet(t, helpers.MTLSClient(t), url)
			require.Equal(t, 201, res.StatusCode)
			require.Equal(t, "WORLD", res.Body)
		})
	}
}

// plainTLSClient returns a client that verifies the server but presents no
// certificate of its own.
func plainTLSClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// httpResult is a response the test is done with: the body is read and closed,
// the rest is what the assertions look at.
type httpResult struct {
	Status     string
	StatusCode int
	ProtoMajor int
	TLS        *tls.ConnectionState
	Header     http.Header
	Body       string
}

// clientGet issues a GET with client and reads the response to completion.
func clientGet(t *testing.T, client *http.Client, url string) httpResult {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return httpResult{
		Status:     resp.Status,
		StatusCode: resp.StatusCode,
		ProtoMajor: resp.ProtoMajor,
		TLS:        resp.TLS,
		Header:     resp.Header,
		Body:       string(body),
	}
}
