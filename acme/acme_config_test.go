package acme

import (
	"strings"
	"testing"
)

func TestConfigInitDefaults(t *testing.T) {
	tests := []struct {
		name              string
		cfg               Config
		wantErr           string
		wantCacheDir      string
		wantChallengeType string
		wantAltHTTPPort   int
	}{
		{
			name:              "minimal config gets every default",
			cfg:               Config{Email: "a@b.c", Domains: []string{"x.com"}},
			wantCacheDir:      "rr_cache_dir",
			wantChallengeType: "http-01",
			wantAltHTTPPort:   80,
		},
		{
			name:    "email is mandatory",
			cfg:     Config{Domains: []string{"x.com"}},
			wantErr: "email could not be empty",
		},
		{
			name:    "at least one domain is mandatory",
			cfg:     Config{Email: "a@b.c"},
			wantErr: "should be at least 1 domain",
		},
		{
			name:              "explicit cache dir is preserved",
			cfg:               Config{Email: "a@b.c", Domains: []string{"x.com"}, CacheDir: "/tmp/le"},
			wantCacheDir:      "/tmp/le",
			wantChallengeType: "http-01",
			wantAltHTTPPort:   80,
		},
		{
			name:              "tlsalpn challenge leaves the http port alone",
			cfg:               Config{Email: "a@b.c", Domains: []string{"x.com"}, ChallengeType: "tlsalpn-01"},
			wantCacheDir:      "rr_cache_dir",
			wantChallengeType: "tlsalpn-01",
			wantAltHTTPPort:   0,
		},
		{
			name:              "explicit alt http port is preserved",
			cfg:               Config{Email: "a@b.c", Domains: []string{"x.com"}, AltHTTPPort: 8080},
			wantCacheDir:      "rr_cache_dir",
			wantChallengeType: "http-01",
			wantAltHTTPPort:   8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			err := cfg.InitDefaults()

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if cfg.CacheDir != tt.wantCacheDir {
				t.Errorf("CacheDir = %q, want %q", cfg.CacheDir, tt.wantCacheDir)
			}
			if cfg.ChallengeType != tt.wantChallengeType {
				t.Errorf("ChallengeType = %q, want %q", cfg.ChallengeType, tt.wantChallengeType)
			}
			if cfg.AltHTTPPort != tt.wantAltHTTPPort {
				t.Errorf("AltHTTPPort = %d, want %d", cfg.AltHTTPPort, tt.wantAltHTTPPort)
			}
		})
	}
}
