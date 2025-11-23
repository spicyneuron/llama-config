package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/spicyneuron/llama-config-proxy/config"
)

func TestCreateServerTimeouts(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		wantIdle    time.Duration
		wantRead    time.Duration
		wantWrite   time.Duration
	}{
		{
			name:      "zero timeout",
			timeout:   0,
			wantIdle:  0,
			wantRead:  0,
			wantWrite: 0,
		},
		{
			name:      "with timeout",
			timeout:   30 * time.Second,
			wantIdle:  30 * time.Second,
			wantRead:  0,
			wantWrite: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.ProxyConfig{
				Listen:  "localhost:8080",
				Target:  "http://localhost:3000",
				Timeout: tt.timeout,
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			server := CreateServer(cfg, handler)

			if server.Addr != cfg.Listen {
				t.Errorf("Server.Addr = %s, want %s", server.Addr, cfg.Listen)
			}

			if server.IdleTimeout != tt.wantIdle {
				t.Errorf("Server.IdleTimeout = %v, want %v", server.IdleTimeout, tt.wantIdle)
			}

			if server.ReadTimeout != tt.wantRead {
				t.Errorf("Server.ReadTimeout = %v, want %v (must be 0 for streaming)", server.ReadTimeout, tt.wantRead)
			}

			if server.WriteTimeout != tt.wantWrite {
				t.Errorf("Server.WriteTimeout = %v, want %v (must be 0 for streaming)", server.WriteTimeout, tt.wantWrite)
			}
		})
	}
}

