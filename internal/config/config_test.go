package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		env     map[string]string
		wantErr bool
		want    *Config
	}{
		{
			name:   "empty config uses defaults",
			config: "",
			want: &Config{
				Server: ServerConfig{
					Address:      "0.0.0.0:8080",
					ReadTimeout:  30 * time.Second,
					WriteTimeout: 30 * time.Second,
					IdleTimeout:  60 * time.Second,
				},
				Upstream: UpstreamConfig{
					Timeout:      30 * time.Second,
					MaxRetries:   3,
					RetryDelay:   time.Second,
					TenantHeader: "X-Prometheus-Tenant",
					TenantLabel:  "tenant_id",
				},
				Auth: AuthConfig{
					JWTAlgorithm:     "RS256",
					TokenTTL:         time.Hour,
					CacheTTL:         5 * time.Minute,
					UserGroupsClaim:  "groups",
				},
				Metrics: MetricsConfig{
					Enabled: true,
					Path:    "/metrics",
				},
				Logging: LoggingConfig{
					Level:  "info",
					Format: "json",
				},
			},
			wantErr: true, // missing required upstream URL
		},
		{
			name:   "missing upstream URL should fail",
			config: "",
			wantErr: true,
		},
		{
			name: "valid config with environment overrides",
			config: `
server:
  address: "127.0.0.1:9090"
upstream:
  url: "http://victoria:8428"
auth:
  type: "jwt"
  jwks_url: "https://auth.example.com/.well-known/jwks.json"
`,
			env: map[string]string{
				"SERVER_ADDRESS":         "0.0.0.0:8080",
				"UPSTREAM_URL":          "http://localhost:8428",
				"AUTH_JWKS_URL":         "https://keycloak.local/jwks",
				"AUTH_REQUIRED_ISSUER":  "https://keycloak.local",
				"AUTH_USER_GROUPS_CLAIM": "realm_access.roles",
				"LOG_LEVEL":             "debug",
			},
			want: &Config{
				Server: ServerConfig{
					Address:      "0.0.0.0:8080", // overridden by env
					ReadTimeout:  30 * time.Second,
					WriteTimeout: 30 * time.Second,
					IdleTimeout:  60 * time.Second,
				},
				Upstream: UpstreamConfig{
					URL:          "http://localhost:8428", // overridden by env
					Timeout:      30 * time.Second,
					MaxRetries:   3,
					RetryDelay:   time.Second,
					TenantHeader: "X-Prometheus-Tenant",
					TenantLabel:  "tenant_id",
				},
				Auth: AuthConfig{
					Type:             "jwt",
					JWKSURL:          "https://keycloak.local", // overridden by env
					JWTAlgorithm:     "RS256",
					RequiredIssuer:   "https://keycloak.local", // from env
					TokenTTL:         time.Hour,
					CacheTTL:         5 * time.Minute,
					UserGroupsClaim:  "realm_access.roles", // overridden by env
				},
				Metrics: MetricsConfig{
					Enabled: true,
					Path:    "/metrics",
				},
				Logging: LoggingConfig{
					Level:  "debug", // overridden by env
					Format: "json",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.env {
				os.Setenv(key, value)
				defer os.Unsetenv(key)
			}

			var configPath string
			if tt.config != "" {
				// Create temporary config file
				tmpFile, err := os.CreateTemp("", "config-*.yaml")
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(tmpFile.Name())

				if _, err := tmpFile.WriteString(tt.config); err != nil {
					t.Fatal(err)
				}
				tmpFile.Close()
				configPath = tmpFile.Name()
			}

			got, err := Load(configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.want != nil && got != nil {
				if got.Server.Address != tt.want.Server.Address {
					t.Errorf("Server.Address = %v, want %v", got.Server.Address, tt.want.Server.Address)
				}
				if got.Upstream.URL != tt.want.Upstream.URL {
					t.Errorf("Upstream.URL = %v, want %v", got.Upstream.URL, tt.want.Upstream.URL)
				}
				if got.Auth.JWKSURL != tt.want.Auth.JWKSURL {
					t.Errorf("Auth.JWKSURL = %v, want %v", got.Auth.JWKSURL, tt.want.Auth.JWKSURL)
				}
				if got.Logging.Level != tt.want.Logging.Level {
					t.Errorf("Logging.Level = %v, want %v", got.Logging.Level, tt.want.Logging.Level)
				}
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Upstream: UpstreamConfig{
					URL: "http://localhost:8428",
				},
				Auth: AuthConfig{
					Type:    "jwt",
					JWKSURL: "https://auth.example.com/jwks",
				},
			},
			wantErr: false,
		},
		{
			name: "missing upstream URL",
			config: &Config{
				Auth: AuthConfig{
					Type:    "jwt",
					JWKSURL: "https://auth.example.com/jwks",
				},
			},
			wantErr: true,
		},
		{
			name: "JWT auth without JWKS URL or secret",
			config: &Config{
				Upstream: UpstreamConfig{
					URL: "http://localhost:8428",
				},
				Auth: AuthConfig{
					Type: "jwt",
				},
			},
			wantErr: true,
		},
		{
			name: "JWT auth with secret is valid",
			config: &Config{
				Upstream: UpstreamConfig{
					URL: "http://localhost:8428",
				},
				Auth: AuthConfig{
					Type:      "jwt",
					JWTSecret: "secret-key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validate(tt.config); (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetDefaults(t *testing.T) {
	config := &Config{}
	setDefaults(config)

	if config.Server.Address != "0.0.0.0:8080" {
		t.Errorf("Expected default server address 0.0.0.0:8080, got %s", config.Server.Address)
	}

	if config.Server.ReadTimeout != 30*time.Second {
		t.Errorf("Expected default read timeout 30s, got %v", config.Server.ReadTimeout)
	}

	if config.Upstream.MaxRetries != 3 {
		t.Errorf("Expected default max retries 3, got %d", config.Upstream.MaxRetries)
	}

	if config.Auth.JWTAlgorithm != "RS256" {
		t.Errorf("Expected default JWT algorithm RS256, got %s", config.Auth.JWTAlgorithm)
	}

	if config.Metrics.Path != "/metrics" {
		t.Errorf("Expected default metrics path /metrics, got %s", config.Metrics.Path)
	}

	if config.Logging.Level != "info" {
		t.Errorf("Expected default log level info, got %s", config.Logging.Level)
	}
}