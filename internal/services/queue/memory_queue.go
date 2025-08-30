package queue

import (
	"context"
	"sync"
	"time"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
)

// Queue utilization constants.
const (
	percentageMultiplier        = 100
	healthyUtilizationThreshold = 90 // Queue is unhealthy if >90% full
)

// MemoryQueue implements a thread-safe in-memory request queue.
type MemoryQueue struct {
	queue         chan *domain.ProxyRequest
	maxSize       int
	timeout       time.Duration
	logger        domain.Logger
	closed        bool
	closeMu       sync.RWMutex
	enqueuedCount int64
	dequeuedCount int64
	droppedCount  int64
	mu            sync.RWMutex
}

// NewMemoryQueue creates a new in-memory request queue.
func NewMemoryQueue(maxSize int, timeout time.Duration, logger domain.Logger) *MemoryQueue {
	return &MemoryQueue{
		queue:   make(chan *domain.ProxyRequest, maxSize),
		maxSize: maxSize,
		timeout: timeout,
		logger:  logger.With(domain.Field{Key: "component", Value: "memory_queue"}),
		closed:  false,
	}
}

// Enqueue adds a request to the queue.
func (mq *MemoryQueue) Enqueue(ctx context.Context, req *domain.ProxyRequest) error {
	mq.closeMu.RLock()
	if mq.closed {
		mq.closeMu.RUnlock()
		return domain.ErrQueueClosed
	}
	mq.closeMu.RUnlock()

	select {
	case mq.queue <- req:
		mq.mu.Lock()
		mq.enqueuedCount++
		enqueuedCount := mq.enqueuedCount
		mq.mu.Unlock()

		mq.logger.Debug("Request enqueued",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "queue_size", Value: len(mq.queue)},
			domain.Field{Key: "enqueued_total", Value: enqueuedCount})
		return nil

	case <-ctx.Done():
		mq.logger.Warn("Request enqueue cancelled due to context",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "error", Value: ctx.Err().Error()})
		return ctx.Err()

	default:
		// Queue is full
		mq.mu.Lock()
		mq.droppedCount++
		droppedCount := mq.droppedCount
		mq.mu.Unlock()

		mq.logger.Warn("Request dropped due to full queue",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "queue_size", Value: len(mq.queue)},
			domain.Field{Key: "max_size", Value: mq.maxSize},
			domain.Field{Key: "dropped_total", Value: droppedCount})

		return domain.ErrQueueFull
	}
}

// Dequeue retrieves the next request from the queue.
func (mq *MemoryQueue) Dequeue(ctx context.Context) (*domain.ProxyRequest, error) {
	mq.closeMu.RLock()
	if mq.closed {
		mq.closeMu.RUnlock()
		return nil, domain.ErrQueueClosed
	}
	mq.closeMu.RUnlock()

	var timeoutChan <-chan time.Time
	if mq.timeout > 0 {
		timer := time.NewTimer(mq.timeout)
		defer timer.Stop()
		timeoutChan = timer.C
	}

	select {
	case req := <-mq.queue:
		mq.mu.Lock()
		mq.dequeuedCount++
		dequeuedCount := mq.dequeuedCount
		mq.mu.Unlock()

		mq.logger.Debug("Request dequeued",
			domain.Field{Key: "user_id", Value: req.User.ID},
			domain.Field{Key: "queue_size", Value: len(mq.queue)},
			domain.Field{Key: "dequeued_total", Value: dequeuedCount})

		return req, nil

	case <-ctx.Done():
		mq.logger.Debug("Request dequeue cancelled due to context",
			domain.Field{Key: "error", Value: ctx.Err().Error()})
		return nil, ctx.Err()

	case <-timeoutChan:
		mq.logger.Debug("Request dequeue timed out",
			domain.Field{Key: "timeout", Value: mq.timeout})
		return nil, domain.ErrQueueTimeout

	default:
		// Non-blocking check - queue is empty
		if timeoutChan == nil {
			return nil, domain.ErrQueueEmpty
		}
		// Continue to blocking select above for timeout case
		select {
		case req := <-mq.queue:
			mq.mu.Lock()
			mq.dequeuedCount++
			dequeuedCount := mq.dequeuedCount
			mq.mu.Unlock()

			mq.logger.Debug("Request dequeued",
				domain.Field{Key: "user_id", Value: req.User.ID},
				domain.Field{Key: "queue_size", Value: len(mq.queue)},
				domain.Field{Key: "dequeued_total", Value: dequeuedCount})

			return req, nil

		case <-ctx.Done():
			return nil, ctx.Err()

		case <-timeoutChan:
			return nil, domain.ErrQueueTimeout
		}
	}
}

// Size returns the current queue size.
func (mq *MemoryQueue) Size() int {
	return len(mq.queue)
}

// Close performs cleanup and graceful shutdown.
func (mq *MemoryQueue) Close() error {
	mq.closeMu.Lock()
	defer mq.closeMu.Unlock()

	if mq.closed {
		return nil // Already closed
	}

	mq.closed = true
	close(mq.queue)

	// Log final statistics
	mq.mu.RLock()
	enqueuedCount := mq.enqueuedCount
	dequeuedCount := mq.dequeuedCount
	droppedCount := mq.droppedCount
	remainingCount := len(mq.queue)
	mq.mu.RUnlock()

	mq.logger.Info("Memory queue closed",
		domain.Field{Key: "total_enqueued", Value: enqueuedCount},
		domain.Field{Key: "total_dequeued", Value: dequeuedCount},
		domain.Field{Key: "total_dropped", Value: droppedCount},
		domain.Field{Key: "remaining_requests", Value: remainingCount})

	return nil
}

// Stats returns queue statistics.
func (mq *MemoryQueue) Stats() Stats {
	mq.mu.RLock()
	defer mq.mu.RUnlock()

	return Stats{
		Size:          len(mq.queue),
		MaxSize:       mq.maxSize,
		EnqueuedTotal: mq.enqueuedCount,
		DequeuedTotal: mq.dequeuedCount,
		DroppedTotal:  mq.droppedCount,
		IsClosed:      mq.closed,
	}
}

// Stats represents queue statistics for monitoring.
type Stats struct {
	Size          int   `json:"size"`
	MaxSize       int   `json:"max_size"`
	EnqueuedTotal int64 `json:"enqueued_total"`
	DequeuedTotal int64 `json:"dequeued_total"`
	DroppedTotal  int64 `json:"dropped_total"`
	IsClosed      bool  `json:"is_closed"`
}

// IsHealthy returns true if the queue is operating normally.
func (qs Stats) IsHealthy() bool {
	if qs.IsClosed {
		return false
	}

	// Consider queue unhealthy if it's consistently near capacity
	utilizationPercent := float64(qs.Size) / float64(qs.MaxSize) * percentageMultiplier
	return utilizationPercent < healthyUtilizationThreshold
}

// UtilizationPercent returns the current queue utilization as a percentage.
func (qs Stats) UtilizationPercent() float64 {
	if qs.MaxSize == 0 {
		return 0
	}
	return float64(qs.Size) / float64(qs.MaxSize) * percentageMultiplier
}
