module tests

go 1.26

toolchain go1.26.0

require (
	github.com/goccy/go-json v0.10.6
	github.com/quic-go/quic-go v0.59.0
	github.com/roadrunner-server/config/v5 v5.1.9
	github.com/roadrunner-server/endure/v2 v2.6.2
	github.com/roadrunner-server/fileserver/v6 v6.0.0-beta.1
	github.com/roadrunner-server/goridge/v4 v4.0.0-beta.1
	github.com/roadrunner-server/gzip/v6 v6.0.0-beta.1
	github.com/roadrunner-server/headers/v5 v5.2.0
	github.com/roadrunner-server/http/v6 v6.0.0-beta.1
	github.com/roadrunner-server/informer/v5 v5.1.9
	github.com/roadrunner-server/logger/v6 v6.0.0-beta.1
	github.com/roadrunner-server/memory/v5 v5.2.9
	github.com/roadrunner-server/pool/v2 v2.0.0-beta.1
	github.com/roadrunner-server/resetter/v5 v5.1.9
	github.com/roadrunner-server/rpc/v5 v5.1.9
	github.com/roadrunner-server/send/v5 v5.2.0
	github.com/roadrunner-server/server/v5 v5.2.10
	github.com/roadrunner-server/static/v5 v5.2.0
	github.com/stretchr/testify v1.11.1
	github.com/yookoala/gofast v0.8.0
	golang.org/x/net v0.52.0
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7
)

require (
	github.com/roadrunner-server/goridge/v3 v3.8.3 // indirect
	github.com/roadrunner-server/pool v1.1.3 // indirect
	go.uber.org/zap v1.27.1 // indirect
)

replace github.com/roadrunner-server/http/v6 => ../

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/caddyserver/certmagic v0.25.2 // indirect
	github.com/caddyserver/zerossl v0.1.5 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/gofiber/fiber/v2 v2.52.12 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/klauspost/compress v1.18.4 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/libdns/libdns v1.1.1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.21 // indirect
	github.com/mholt/acmez v1.2.0 // indirect
	github.com/mholt/acmez/v3 v3.1.6 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.67.5 // indirect
	github.com/prometheus/procfs v0.20.1 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/roadrunner-server/api-go/v6 v6.0.0-beta.1 // indirect
	github.com/roadrunner-server/api-plugins/v6 v6.0.0-beta.2 // indirect
	github.com/roadrunner-server/api/v4 v4.23.0 // indirect
	github.com/roadrunner-server/context v1.3.0
	github.com/roadrunner-server/errors v1.4.1 // indirect
	github.com/roadrunner-server/events v1.0.1 // indirect
	github.com/roadrunner-server/tcplisten v1.5.2 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/sagikazarmark/locafero v0.12.0 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/spf13/viper v1.21.0 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.69.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/blake3 v0.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0
	go.opentelemetry.io/contrib/propagators/jaeger v1.42.0 // indirect
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap/exp v0.3.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	golang.org/x/tools/godoc v0.1.0-deprecated // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
