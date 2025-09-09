package storage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/config/modules/storage"
)

func TestStorageConfig_Validate_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config storage.Config
	}{
		{
			name: "valid local storage",
			config: storage.Config{
				Type: "local",
			},
		},
		{
			name: "valid redis storage",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address:   "redis:6379",
					Password:  "",
					Database:  0,
					KeyPrefix: "vm-proxy-auth:",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 5 * time.Second,
						Read:    3 * time.Second,
						Write:   3 * time.Second,
					},
					Retry: storage.RedisRetryConfig{
						MaxAttempts: 3,
						Backoff: storage.BackoffConfig{
							Min: 100 * time.Millisecond,
							Max: 1 * time.Second,
						},
					},
				},
			},
		},
		{
			name: "valid raft storage",
			config: storage.Config{
				Type: "raft",
				Raft: storage.RaftConfig{
					BindAddress:       "0.0.0.0:9000",
					DataDir:           "/data/raft",
					BootstrapExpected: 3,
					Peers:             []string{}, // Don't specify peers when using bootstrap expected
				},
			},
		},
		{
			name: "valid raft storage without peers (optional)",
			config: storage.Config{
				Type: "raft",
				Raft: storage.RaftConfig{
					BindAddress:       "0.0.0.0:9000",
					DataDir:           "/data/raft",
					BootstrapExpected: 1,
					Peers:             []string{}, // Empty peers should be valid
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestStorageConfig_Validate_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      storage.Config
		expectedErr string
	}{
		{
			name: "invalid storage type",
			config: storage.Config{
				Type: "invalid",
			},
			expectedErr: "invalid storage type: invalid",
		},
		{
			name: "redis without address",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "",
				},
			},
			expectedErr: "redis address is required",
		},
		{
			name: "redis with negative database",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address:  "redis:6379",
					Database: -1,
				},
			},
			expectedErr: "redis database must be non-negative",
		},
		{
			name: "redis with zero pool size",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    0,
						MinIdle: 5,
					},
				},
			},
			expectedErr: "pool size must be positive",
		},
		{
			name: "redis with negative min idle",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: -1,
					},
				},
			},
			expectedErr: "cannot be negative",
		},
		{
			name: "redis with min idle > pool size",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    5,
						MinIdle: 10,
					},
				},
			},
			expectedErr: "cannot exceed pool size",
		},
		{
			name: "redis with zero connect timeout",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 0,
					},
				},
			},
			expectedErr: "connect timeout must be positive",
		},
		{
			name: "redis with zero read timeout",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 5 * time.Second,
						Read:    0,
					},
				},
			},
			expectedErr: "read timeout must be positive",
		},
		{
			name: "redis with zero write timeout",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 5 * time.Second,
						Read:    3 * time.Second,
						Write:   0,
					},
				},
			},
			expectedErr: "write timeout must be positive",
		},
		{
			name: "redis with zero max backoff",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 5 * time.Second,
						Read:    3 * time.Second,
						Write:   3 * time.Second,
					},
					Retry: storage.RedisRetryConfig{
						MaxAttempts: 3,
						Backoff: storage.BackoffConfig{
							Min: 100 * time.Millisecond,
							Max: 0,
						},
					},
				},
			},
			expectedErr: "cannot exceed max backoff",
		},
		{
			name: "redis with min backoff > max backoff",
			config: storage.Config{
				Type: "redis",
				Redis: storage.RedisConfig{
					Address: "redis:6379",
					Pool: storage.RedisPoolConfig{
						Size:    10,
						MinIdle: 5,
					},
					Timeouts: storage.RedisTimeoutsConfig{
						Connect: 5 * time.Second,
						Read:    3 * time.Second,
						Write:   3 * time.Second,
					},
					Retry: storage.RedisRetryConfig{
						MaxAttempts: 3,
						Backoff: storage.BackoffConfig{
							Min: 2 * time.Second,
							Max: 1 * time.Second,
						},
					},
				},
			},
			expectedErr: "cannot exceed max backoff",
		},
		{
			name: "raft without bind address",
			config: storage.Config{
				Type: "raft",
				Raft: storage.RaftConfig{
					BindAddress: "",
				},
			},
			expectedErr: "raft bind address is required",
		},
		{
			name: "raft without data directory",
			config: storage.Config{
				Type: "raft",
				Raft: storage.RaftConfig{
					BindAddress: "0.0.0.0:9000",
					DataDir:     "",
				},
			},
			expectedErr: "raft data directory is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestStorageConfig_Defaults(t *testing.T) {
	t.Parallel()

	defaults := storage.GetDefaults()

	assert.Equal(t, "local", defaults.Type)
	assert.Equal(t, 0, defaults.Redis.Database)
	assert.Equal(t, "vm-proxy-auth:", defaults.Redis.KeyPrefix)
	assert.Equal(t, 20, defaults.Redis.Pool.Size)
	assert.Equal(t, 10, defaults.Redis.Pool.MinIdle)
	assert.Equal(t, 5*time.Second, defaults.Redis.Timeouts.Connect)
	assert.Equal(t, 3*time.Second, defaults.Redis.Timeouts.Read)
	assert.Equal(t, 3*time.Second, defaults.Redis.Timeouts.Write)
	assert.Equal(t, 3, defaults.Redis.Retry.MaxAttempts)
	assert.Equal(t, 100*time.Millisecond, defaults.Redis.Retry.Backoff.Min)
	assert.Equal(t, 1*time.Second, defaults.Redis.Retry.Backoff.Max)
	assert.Equal(t, "127.0.0.1:9000", defaults.Raft.BindAddress)
	assert.Equal(t, "/data/raft", defaults.Raft.DataDir)
	assert.Equal(t, 0, defaults.Raft.BootstrapExpected)
}

func TestRedisConfig_ValidationEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		config      storage.RedisConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid with empty password",
			config: storage.RedisConfig{
				Address:   "redis:6379",
				Password:  "",
				Database:  0,
				KeyPrefix: "",
				Pool: storage.RedisPoolConfig{
					Size:    1,
					MinIdle: 0,
				},
				Timeouts: storage.RedisTimeoutsConfig{
					Connect: 1 * time.Second,
					Read:    1 * time.Second,
					Write:   1 * time.Second,
				},
				Retry: storage.RedisRetryConfig{
					MaxAttempts: 1,
					Backoff: storage.BackoffConfig{
						Min: 1 * time.Millisecond,
						Max: 1 * time.Millisecond,
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid with high database number",
			config: storage.RedisConfig{
				Address:  "redis:6379",
				Database: 15,
				Pool: storage.RedisPoolConfig{
					Size:    10,
					MinIdle: 5,
				},
				Timeouts: storage.RedisTimeoutsConfig{
					Connect: 5 * time.Second,
					Read:    3 * time.Second,
					Write:   3 * time.Second,
				},
				Retry: storage.RedisRetryConfig{
					MaxAttempts: 3,
					Backoff: storage.BackoffConfig{
						Min: 100 * time.Millisecond,
						Max: 1 * time.Second,
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.config.Validate()
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
