package tests

import (
	"context"
	"net/http"
	"testing"

	"tests/helpers"

	rrcontext "github.com/roadrunner-server/context"
	"github.com/roadrunner-server/gzip/v6"
	httpPlugin "github.com/roadrunner-server/http/v6"
	"github.com/roadrunner-server/server/v6"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelglobal "go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Each middleware in the chain gets a span of its own, named after the plugin.
func TestHTTPOTLP(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  string
	}{
		{name: "goWorker", cfg: "configs/.rr-http-otel.yaml"},
		{name: "phpWorker", cfg: "configs/.rr-http-otel2.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			exp := setupTracetest(t)

			_, stop := helpers.Start(t, tt.cfg, []any{
				&server.Plugin{},
				&gzip.Plugin{},
				&httpPlugin.Plugin{},
				&tracetestMiddleware{},
			}, helpers.WithTCPProbe("127.0.0.1:43239"))

			code, _ := helpers.GetBody(t, "http://127.0.0.1:43239")
			require.Equal(t, http.StatusOK, code)

			stop()

			spans := exp.GetSpans()
			require.GreaterOrEqual(t, len(spans), 2)

			names := make([]string, len(spans))
			for i, s := range spans {
				names[i] = s.Name
			}

			require.Contains(t, names, "http")
			require.Contains(t, names, "gzip")
		})
	}
}

// tracetestMiddleware replaces otel.Plugin in tests. It creates a root span via
// otelhttp (using the global TracerProvider set by setupTracetest) and injects
// OtelTracerNameKey so the HTTP plugin activates its own child-span logic.
type tracetestMiddleware struct{}

func (m *tracetestMiddleware) Init() error  { return nil }
func (m *tracetestMiddleware) Name() string { return "tracetestOtel" }

func (m *tracetestMiddleware) Middleware(next http.Handler) http.Handler {
	return otelhttp.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), rrcontext.OtelTracerNameKey, m.Name())
			next.ServeHTTP(w, r.WithContext(ctx))
		}),
		"http-server",
	)
}

// setupTracetest installs an in-memory exporter as the global TracerProvider.
// Spans are exported synchronously (WithSyncer), so all spans are available
// immediately after the server shuts down.
func setupTracetest(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otelglobal.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	return exp
}
