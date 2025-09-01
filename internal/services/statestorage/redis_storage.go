package statestorage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Redis storage constants.
const (
	defaultRedisConnectTimeout = 5 * time.Second
	defaultRedisReadTimeout    = 3 * time.Second
	defaultRedisWriteTimeout   = 3 * time.Second
	defaultRedisPoolSize       = 10
	defaultRedisMinIdleConns   = 5
	defaultKeyPrefix           = "vm-proxy-auth:"
	defaultPubSubChannel       = "vm-proxy-auth:events"
	redisWatchChannelBuffer    = 100
)

// RedisStorageConfig holds configuration for Redis storage.
type RedisStorageConfig struct {
	Address         string
	Password        string
	Database        int
	KeyPrefix       string
	ConnectTimeout  time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	PoolSize        int
	MinIdleConns    int
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
}

// RedisStorage provides Redis-based distributed state storage.
type RedisStorage struct {
	client     *redis.Client
	config     RedisStorageConfig
	nodeID     string
	logger     domain.Logger
	pubsub     *redis.PubSub
	watchers   map[string][]chan domain.StateEvent
	watchersMu sync.RWMutex
	closed     bool
	closeMu    sync.RWMutex
	closeOnce  sync.Once
}

// NewRedisStorage creates a new Redis storage instance with connection validation.
func NewRedisStorage(
	config RedisStorageConfig,
	nodeID string,
	logger domain.Logger,
) (*RedisStorage, error) {
	if config.Address == "" {
		return nil, fmt.Errorf("redis address is required")
	}

	if config.KeyPrefix == "" {
		config.KeyPrefix = defaultKeyPrefix
	}

	// Set defaults
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = defaultRedisConnectTimeout
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = defaultRedisReadTimeout
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = defaultRedisWriteTimeout
	}
	if config.PoolSize == 0 {
		config.PoolSize = defaultRedisPoolSize
	}
	if config.MinIdleConns == 0 {
		config.MinIdleConns = defaultRedisMinIdleConns
	}

	// Create Redis client with optimized settings
	client := redis.NewClient(&redis.Options{
		Addr:            config.Address,
		Password:        config.Password,
		DB:              config.Database,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,
		DialTimeout:     config.ConnectTimeout,
		ReadTimeout:     config.ReadTimeout,
		WriteTimeout:    config.WriteTimeout,
		PoolSize:        config.PoolSize,
		MinIdleConns:    config.MinIdleConns,
		PoolTimeout:     config.ConnectTimeout,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.ConnectTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	storage := &RedisStorage{
		client:   client,
		config:   config,
		nodeID:   nodeID,
		logger:   logger,
		watchers: make(map[string][]chan domain.StateEvent),
	}

	// Start PubSub monitoring for state events
	if err := storage.startPubSubMonitoring(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to start PubSub monitoring: %w", err)
	}

	logger.Info("Redis storage initialized",
		domain.Field{Key: "address", Value: config.Address},
		domain.Field{Key: "database", Value: config.Database},
		domain.Field{Key: "key_prefix", Value: config.KeyPrefix},
		domain.Field{Key: "node_id", Value: nodeID})

	return storage, nil
}

// Get retrieves a value by key from Redis.
func (rs *RedisStorage) Get(ctx context.Context, key string) ([]byte, error) {
	if err := rs.checkClosed(); err != nil {
		return nil, err
	}

	redisKey := rs.formatKey(key)

	result, err := rs.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, domain.ErrKeyNotFound
		}
		rs.logger.Error("Redis GET failed",
			domain.Field{Key: "key", Value: key},
			domain.Field{Key: "redis_key", Value: redisKey},
			domain.Field{Key: "error", Value: err})
		return nil, fmt.Errorf("failed to get key %q: %w", key, err)
	}

	rs.logger.Debug("Redis GET successful",
		domain.Field{Key: "key", Value: key},
		domain.Field{Key: "value_size", Value: len(result)})

	return result, nil
}

// Set stores a value with optional TTL in Redis.
func (rs *RedisStorage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := rs.checkClosed(); err != nil {
		return err
	}

	redisKey := rs.formatKey(key)

	// Store with TTL if specified
	var err error
	if ttl > 0 {
		err = rs.client.SetEx(ctx, redisKey, value, ttl).Err()
	} else {
		err = rs.client.Set(ctx, redisKey, value, 0).Err()
	}

	if err != nil {
		rs.logger.Error("Redis SET failed",
			domain.Field{Key: "key", Value: key},
			domain.Field{Key: "redis_key", Value: redisKey},
			domain.Field{Key: "value_size", Value: len(value)},
			domain.Field{Key: "ttl", Value: ttl},
			domain.Field{Key: "error", Value: err})
		return fmt.Errorf("failed to set key %q: %w", key, err)
	}

	// Publish state change event
	rs.publishStateEvent(domain.StateEvent{
		Type:      domain.StateEventSet,
		Key:       key,
		Value:     value,
		Timestamp: time.Now(),
		NodeID:    rs.nodeID,
	})

	rs.logger.Debug("Redis SET successful",
		domain.Field{Key: "key", Value: key},
		domain.Field{Key: "value_size", Value: len(value)},
		domain.Field{Key: "ttl", Value: ttl})

	return nil
}

// Delete removes a key from Redis.
func (rs *RedisStorage) Delete(ctx context.Context, key string) error {
	if err := rs.checkClosed(); err != nil {
		return err
	}

	redisKey := rs.formatKey(key)

	deleted, err := rs.client.Del(ctx, redisKey).Result()
	if err != nil {
		rs.logger.Error("Redis DEL failed",
			domain.Field{Key: "key", Value: key},
			domain.Field{Key: "redis_key", Value: redisKey},
			domain.Field{Key: "error", Value: err})
		return fmt.Errorf("failed to delete key %q: %w", key, err)
	}

	if deleted > 0 {
		// Publish state change event
		rs.publishStateEvent(domain.StateEvent{
			Type:      domain.StateEventDelete,
			Key:       key,
			Timestamp: time.Now(),
			NodeID:    rs.nodeID,
		})

		rs.logger.Debug("Redis DEL successful",
			domain.Field{Key: "key", Value: key},
			domain.Field{Key: "deleted_count", Value: deleted})
	}

	return nil
}

// GetMultiple retrieves multiple values efficiently using Redis pipeline.
func (rs *RedisStorage) GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error) {
	if err := rs.checkClosed(); err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return make(map[string][]byte), nil
	}

	// Use Redis pipeline for efficient bulk operations
	pipe := rs.client.Pipeline()

	redisKeys := make([]string, len(keys))
	for i, key := range keys {
		redisKeys[i] = rs.formatKey(key)
	}

	// Add all GET commands to pipeline
	cmds := make([]*redis.StringCmd, len(redisKeys))
	for i, redisKey := range redisKeys {
		cmds[i] = pipe.Get(ctx, redisKey)
	}

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		rs.logger.Error("Redis pipeline GET failed",
			domain.Field{Key: "key_count", Value: len(keys)},
			domain.Field{Key: "error", Value: err})
		return nil, fmt.Errorf("failed to execute pipeline GET: %w", err)
	}

	// Collect results
	result := make(map[string][]byte)
	for i, cmd := range cmds {
		value, cmdErr := cmd.Bytes()
		if cmdErr == nil {
			result[keys[i]] = value
		}
		// Ignore redis.Nil errors - missing keys are simply omitted
	}

	rs.logger.Debug("Redis pipeline GET completed",
		domain.Field{Key: "requested_keys", Value: len(keys)},
		domain.Field{Key: "found_keys", Value: len(result)})

	return result, nil
}

// SetMultiple stores multiple values efficiently using Redis pipeline.
func (rs *RedisStorage) SetMultiple(
	ctx context.Context,
	items map[string][]byte,
	ttl time.Duration,
) error {
	if err := rs.checkClosed(); err != nil {
		return err
	}

	if len(items) == 0 {
		return nil
	}

	// Use Redis pipeline for efficient bulk operations
	pipe := rs.client.Pipeline()

	for key, value := range items {
		redisKey := rs.formatKey(key)
		if ttl > 0 {
			pipe.SetEx(ctx, redisKey, value, ttl)
		} else {
			pipe.Set(ctx, redisKey, value, 0)
		}
	}

	// Execute pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		rs.logger.Error("Redis pipeline SET failed",
			domain.Field{Key: "item_count", Value: len(items)},
			domain.Field{Key: "ttl", Value: ttl},
			domain.Field{Key: "error", Value: err})
		return fmt.Errorf("failed to execute pipeline SET: %w", err)
	}

	// Publish state change events for all items
	timestamp := time.Now()
	for key, value := range items {
		rs.publishStateEvent(domain.StateEvent{
			Type:      domain.StateEventSet,
			Key:       key,
			Value:     value,
			Timestamp: timestamp,
			NodeID:    rs.nodeID,
		})
	}

	rs.logger.Debug("Redis pipeline SET completed",
		domain.Field{Key: "item_count", Value: len(items)},
		domain.Field{Key: "ttl", Value: ttl})

	return nil
}

// Watch observes changes to keys matching the prefix using Redis pub/sub.
func (rs *RedisStorage) Watch(
	ctx context.Context,
	keyPrefix string,
) (<-chan domain.StateEvent, error) {
	if err := rs.checkClosed(); err != nil {
		return nil, err
	}

	ch := make(chan domain.StateEvent, redisWatchChannelBuffer)

	rs.watchersMu.Lock()
	rs.watchers[keyPrefix] = append(rs.watchers[keyPrefix], ch)
	rs.watchersMu.Unlock()

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		rs.removeWatcherAndClose(keyPrefix, ch)
	}()

	rs.logger.Debug("Redis watcher registered",
		domain.Field{Key: "key_prefix", Value: keyPrefix},
		domain.Field{Key: "channel_buffer", Value: redisWatchChannelBuffer})

	return ch, nil
}

// Close performs cleanup and graceful shutdown.
func (rs *RedisStorage) Close() error {
	var returnErr error

	rs.closeOnce.Do(func() {
		rs.closeMu.Lock()
		rs.closed = true
		rs.closeMu.Unlock()

		// Close PubSub subscription
		if rs.pubsub != nil {
			if err := rs.pubsub.Close(); err != nil {
				rs.logger.Error("Failed to close Redis PubSub", domain.Field{Key: "error", Value: err})
				returnErr = fmt.Errorf("failed to close PubSub: %w", err)
			}
		}

		// Close all watchers
		rs.closeAllWatchers()

		// Close Redis connection
		if err := rs.client.Close(); err != nil {
			rs.logger.Error("Failed to close Redis client", domain.Field{Key: "error", Value: err})
			if returnErr == nil {
				returnErr = fmt.Errorf("failed to close Redis client: %w", err)
			}
		}

		rs.logger.Info("Redis storage closed",
			domain.Field{Key: "node_id", Value: rs.nodeID},
			domain.Field{Key: "address", Value: rs.config.Address})
	})

	return returnErr
}

// Ping checks the health of the Redis connection.
func (rs *RedisStorage) Ping(ctx context.Context) error {
	if err := rs.checkClosed(); err != nil {
		return err
	}

	if err := rs.client.Ping(ctx).Err(); err != nil {
		rs.logger.Error("Redis ping failed", domain.Field{Key: "error", Value: err})
		return fmt.Errorf("Redis ping failed: %w", err)
	}

	return nil
}

// formatKey applies the configured key prefix.
func (rs *RedisStorage) formatKey(key string) string {
	return rs.config.KeyPrefix + key
}

// checkClosed returns an error if the storage is closed.
func (rs *RedisStorage) checkClosed() error {
	rs.closeMu.RLock()
	defer rs.closeMu.RUnlock()

	if rs.closed {
		return domain.ErrStorageUnavailable
	}
	return nil
}

// startPubSubMonitoring initializes Redis pub/sub for distributed state events.
func (rs *RedisStorage) startPubSubMonitoring() error {
	rs.pubsub = rs.client.Subscribe(context.Background(), defaultPubSubChannel)

	// Verify subscription
	_, err := rs.pubsub.Receive(context.Background())
	if err != nil {
		return fmt.Errorf("failed to subscribe to Redis pub/sub: %w", err)
	}

	// Start message processing goroutine
	go rs.processPubSubMessages()

	return nil
}

// processPubSubMessages handles incoming state events from Redis pub/sub.
func (rs *RedisStorage) processPubSubMessages() {
	defer func() {
		if r := recover(); r != nil {
			rs.logger.Error("Redis PubSub message processor panic recovered",
				domain.Field{Key: "panic", Value: r})
		}
	}()

	for {
		select {
		case <-time.After(time.Second):
			// Check if closed
			if rs.isClosed() {
				return
			}

			// Non-blocking receive
			msg, err := rs.pubsub.ReceiveTimeout(context.Background(), 100*time.Millisecond)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					continue // Normal timeout, keep checking
				}
				if rs.isClosed() {
					return
				}
				rs.logger.Error("Redis PubSub receive error", domain.Field{Key: "error", Value: err})
				continue
			}

			if pubSubMsg, ok := msg.(*redis.Message); ok {
				rs.handlePubSubMessage(pubSubMsg)
			}
		}
	}
}

// handlePubSubMessage processes a Redis pub/sub message.
func (rs *RedisStorage) handlePubSubMessage(msg *redis.Message) {
	var event domain.StateEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		rs.logger.Error("Failed to unmarshal state event",
			domain.Field{Key: "payload", Value: msg.Payload},
			domain.Field{Key: "error", Value: err})
		return
	}

	// Skip events from this node to avoid loops
	if event.NodeID == rs.nodeID {
		return
	}

	if !event.IsValid() {
		rs.logger.Warn("Invalid state event received",
			domain.Field{Key: "event", Value: event})
		return
	}

	// Notify local watchers
	rs.notifyWatchers(event.Key, event)

	rs.logger.Debug("Redis state event processed",
		domain.Field{Key: "type", Value: event.Type},
		domain.Field{Key: "key", Value: event.Key},
		domain.Field{Key: "source_node", Value: event.NodeID})
}

// publishStateEvent publishes a state change event to Redis pub/sub.
func (rs *RedisStorage) publishStateEvent(event domain.StateEvent) {
	if rs.isClosed() {
		return
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		rs.logger.Error("Failed to marshal state event",
			domain.Field{Key: "event", Value: event},
			domain.Field{Key: "error", Value: err})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), rs.config.WriteTimeout)
	defer cancel()

	if err := rs.client.Publish(ctx, defaultPubSubChannel, eventJSON).Err(); err != nil {
		rs.logger.Error("Failed to publish state event",
			domain.Field{Key: "event", Value: event},
			domain.Field{Key: "error", Value: err})
		return
	}

	rs.logger.Debug("Redis state event published",
		domain.Field{Key: "type", Value: event.Type},
		domain.Field{Key: "key", Value: event.Key},
		domain.Field{Key: "node_id", Value: event.NodeID})
}

// notifyWatchers sends events to local watchers with matching key prefixes.
func (rs *RedisStorage) notifyWatchers(key string, event domain.StateEvent) {
	if rs.isClosed() {
		return
	}

	rs.watchersMu.RLock()
	defer rs.watchersMu.RUnlock()

	for prefix, channels := range rs.watchers {
		if strings.HasPrefix(key, prefix) {
			for _, ch := range channels {
				select {
				case ch <- event:
					// Event sent successfully
				default:
					// Channel full, skip to avoid blocking
					rs.logger.Warn("State event channel full, dropping event",
						domain.Field{Key: "key_prefix", Value: prefix},
						domain.Field{Key: "event_key", Value: key},
						domain.Field{Key: "event_type", Value: event.Type})
				}
			}
		}
	}
}

// removeWatcherAndClose removes a watcher and closes its channel safely.
func (rs *RedisStorage) removeWatcherAndClose(keyPrefix string, ch chan domain.StateEvent) {
	rs.watchersMu.Lock()
	defer rs.watchersMu.Unlock()

	channels, exists := rs.watchers[keyPrefix]
	if !exists {
		return
	}

	// Remove channel from slice
	for i, existingCh := range channels {
		if existingCh == ch {
			rs.watchers[keyPrefix] = append(channels[:i], channels[i+1:]...)
			break
		}
	}

	// Remove prefix entry if no more channels
	if len(rs.watchers[keyPrefix]) == 0 {
		delete(rs.watchers, keyPrefix)
	}

	// Close channel safely
	select {
	case <-ch:
		// Channel already closed
	default:
		close(ch)
	}

	rs.logger.Debug("Redis watcher removed",
		domain.Field{Key: "key_prefix", Value: keyPrefix},
		domain.Field{Key: "remaining_watchers", Value: len(rs.watchers[keyPrefix])})
}

// closeAllWatchers closes all active watchers.
func (rs *RedisStorage) closeAllWatchers() {
	rs.watchersMu.Lock()
	defer rs.watchersMu.Unlock()

	for prefix, channels := range rs.watchers {
		for _, ch := range channels {
			select {
			case <-ch:
				// Channel already closed
			default:
				close(ch)
			}
		}
		delete(rs.watchers, prefix)
	}

	rs.logger.Debug("All Redis watchers closed",
		domain.Field{Key: "node_id", Value: rs.nodeID})
}

// isClosed checks if storage is closed (thread-safe).
func (rs *RedisStorage) isClosed() bool {
	rs.closeMu.RLock()
	defer rs.closeMu.RUnlock()
	return rs.closed
}
