package config

import "testing"

func TestInheritHost(t *testing.T) {
	tests := []struct{ http, ssl, want string }{
		{"[::]:8080", ":8443", "[::]:8443"},
		{"tcp://[::1]:8080", ":8443", "[::1]:8443"},
		{"0.0.0.0:8080", ":8443", "0.0.0.0:8443"},
		{"localhost:8080", ":8443", "localhost:8443"},
		{"[::]:8080", "", "[::]:443"},
		{":8080", ":8443", ":8443"},
		{":8080", "", ":443"},
		{"[::]:8080", "127.0.0.1:8443", "127.0.0.1:8443"},
		{"unix:///tmp/rr.sock", ":8443", ":8443"},
	}
	for _, tt := range tests {
		if got := InheritHost(tt.http, tt.ssl); got != tt.want {
			t.Errorf("InheritHost(%q, %q) = %q, want %q", tt.http, tt.ssl, got, tt.want)
		}
	}
}
