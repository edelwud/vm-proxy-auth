package queue_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edelwud/vm-proxy-auth/internal/domain"
	"github.com/edelwud/vm-proxy-auth/internal/services/queue"
	"github.com/edelwud/vm-proxy-auth/internal/testutils"
)

func createTestRequest(userID string) *domain.ProxyRequest {
	return &domain.ProxyRequest{
		User: &domain.User{
			ID:             userID,
			AllowedTenants: []string{"1000"},
		},
		OriginalRequest: &http.Request{},
		FilteredQuery:   "up{vm_account_id=\"1000\"}",
		TargetTenant:    "1000",
	}
}

func TestMemoryQueue_EnqueueDequeue(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(10, time.Second, logger)
	defer q.Close()

	ctx := context.Background()
	req := createTestRequest("test-user")

	// Test enqueue
	err := q.Enqueue(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 1, q.Size())

	// Test dequeue
	dequeuedReq, err := q.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, req, dequeuedReq)
	assert.Equal(t, 0, q.Size())
}

func TestMemoryQueue_QueueFull(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(2, time.Second, logger) // Small queue
	defer q.Close()

	ctx := context.Background()

	// Fill queue to capacity
	req1 := createTestRequest("user1")
	req2 := createTestRequest("user2")
	
	err := q.Enqueue(ctx, req1)
	require.NoError(t, err)
	err = q.Enqueue(ctx, req2)
	require.NoError(t, err)
	
	assert.Equal(t, 2, q.Size())

	// Try to enqueue when full
	req3 := createTestRequest("user3")
	err = q.Enqueue(ctx, req3)
	assert.Equal(t, domain.ErrQueueFull, err)
	assert.Equal(t, 2, q.Size()) // Size unchanged

	// Verify stats
	stats := q.Stats()
	assert.Equal(t, 2, stats.Size)
	assert.Equal(t, 2, stats.MaxSize)
	assert.Equal(t, int64(2), stats.EnqueuedTotal)
	assert.Equal(t, int64(0), stats.DequeuedTotal)
	assert.Equal(t, int64(1), stats.DroppedTotal)
}

func TestMemoryQueue_DequeueEmpty(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(10, 0, logger) // No timeout
	defer q.Close()

	ctx := context.Background()

	// Try to dequeue from empty queue (non-blocking)
	_, err := q.Dequeue(ctx)
	assert.Equal(t, domain.ErrQueueEmpty, err)
}

func TestMemoryQueue_DequeueTimeout(t *testing.T) {
	logger := testutils.NewMockLogger()
	timeout := 50 * time.Millisecond
	q := queue.NewMemoryQueue(10, timeout, logger)
	defer q.Close()

	ctx := context.Background()

	start := time.Now()
	_, err := q.Dequeue(ctx)
	duration := time.Since(start)

	assert.Equal(t, domain.ErrQueueTimeout, err)
	assert.GreaterOrEqual(t, duration, timeout)
	assert.Less(t, duration, timeout+20*time.Millisecond) // Allow some tolerance
}

func TestMemoryQueue_ContextCancellation(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(10, time.Second, logger)
	defer q.Close()

	// Test enqueue cancellation - fill queue first to make enqueue block
	ctx := context.Background()
	req := createTestRequest("test-user")
	
	// Fill queue to capacity
	for i := 0; i < 10; i++ {
		err := q.Enqueue(ctx, req)
		require.NoError(t, err)
	}

	// Now test enqueue with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := q.Enqueue(cancelledCtx, req)
	assert.Equal(t, context.Canceled, err)

	// Test dequeue cancellation with empty queue and timeout context
	emptyQ := queue.NewMemoryQueue(10, time.Second, logger)
	defer emptyQ.Close()

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()

	_, err = emptyQ.Dequeue(ctx2)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestMemoryQueue_ConcurrentAccess(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(50, 0, logger) // No timeout for simplicity
	defer q.Close()

	ctx := context.Background()
	const numProducers = 5
	const numConsumers = 3
	const itemsPerProducer = 10

	var producerWg sync.WaitGroup
	var consumerWg sync.WaitGroup
	
	results := make(chan *domain.ProxyRequest, numProducers*itemsPerProducer)
	done := make(chan struct{})

	// Start consumers first
	for i := 0; i < numConsumers; i++ {
		consumerWg.Add(1)
		go func(consumerID int) {
			defer consumerWg.Done()
			for {
				select {
				case <-done:
					return
				default:
					req, err := q.Dequeue(ctx)
					if err == domain.ErrQueueEmpty {
						time.Sleep(1 * time.Millisecond)
						continue
					}
					if err != nil {
						t.Errorf("Consumer %d dequeue error: %v", consumerID, err)
						return
					}
					results <- req
				}
			}
		}(i)
	}

	// Start producers
	for i := 0; i < numProducers; i++ {
		producerWg.Add(1)
		go func(producerID int) {
			defer producerWg.Done()
			for j := 0; j < itemsPerProducer; j++ {
				req := createTestRequest(fmt.Sprintf("producer-%d-item-%d", producerID, j))
				for {
					err := q.Enqueue(ctx, req)
					if err == domain.ErrQueueFull {
						time.Sleep(1 * time.Millisecond)
						continue
					}
					if err != nil {
						t.Errorf("Producer %d enqueue error: %v", producerID, err)
						return
					}
					break
				}
			}
		}(i)
	}

	// Wait for producers to finish
	producerWg.Wait()

	// Collect all results
	expectedItems := numProducers * itemsPerProducer
	processed := 0
	timeout := time.After(2 * time.Second)

	for processed < expectedItems {
		select {
		case <-results:
			processed++
		case <-timeout:
			t.Fatalf("Timeout waiting for items. Processed: %d/%d, Queue size: %d", processed, expectedItems, q.Size())
		}
	}

	// Signal consumers to stop
	close(done)
	consumerWg.Wait()

	// Verify final stats
	stats := q.Stats()
	assert.Equal(t, int64(expectedItems), stats.EnqueuedTotal)
	assert.Equal(t, int64(expectedItems), stats.DequeuedTotal)
	t.Logf("Stats - Enqueued: %d, Dequeued: %d, Dropped: %d", 
		stats.EnqueuedTotal, stats.DequeuedTotal, stats.DroppedTotal)
}

func TestMemoryQueue_Close(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(10, time.Second, logger)

	ctx := context.Background()
	req := createTestRequest("test-user")

	// Add some items
	q.Enqueue(ctx, req)
	q.Enqueue(ctx, req)
	assert.Equal(t, 2, q.Size())

	// Close queue
	err := q.Close()
	assert.NoError(t, err)

	// Should be able to close multiple times
	err = q.Close()
	assert.NoError(t, err)

	// Operations should fail after close
	err = q.Enqueue(ctx, req)
	assert.Equal(t, domain.ErrQueueClosed, err)

	_, err = q.Dequeue(ctx)
	assert.Equal(t, domain.ErrQueueClosed, err)

	// Stats should still work
	stats := q.Stats()
	assert.True(t, stats.IsClosed)
	assert.Equal(t, int64(2), stats.EnqueuedTotal)
}

func TestMemoryQueue_Stats(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(5, time.Second, logger)
	defer q.Close()

	ctx := context.Background()

	// Initially empty
	stats := q.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, 5, stats.MaxSize)
	assert.Equal(t, int64(0), stats.EnqueuedTotal)
	assert.Equal(t, int64(0), stats.DequeuedTotal)
	assert.Equal(t, int64(0), stats.DroppedTotal)
	assert.False(t, stats.IsClosed)
	assert.True(t, stats.IsHealthy()) // Empty queue is healthy

	// Add some items
	for i := 0; i < 3; i++ {
		req := createTestRequest(fmt.Sprintf("user-%d", i))
		q.Enqueue(ctx, req)
	}

	stats = q.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, int64(3), stats.EnqueuedTotal)
	assert.Equal(t, 60.0, stats.UtilizationPercent()) // 3/5 * 100 = 60%
	assert.True(t, stats.IsHealthy()) // 60% is healthy

	// Fill queue nearly to capacity
	for i := 3; i < 5; i++ {
		req := createTestRequest(fmt.Sprintf("user-%d", i))
		q.Enqueue(ctx, req)
	}

	stats = q.Stats()
	assert.Equal(t, 5, stats.Size)
	assert.Equal(t, 100.0, stats.UtilizationPercent()) // 5/5 * 100 = 100%
	assert.False(t, stats.IsHealthy()) // 100% is unhealthy (>= 90%)

	// Try to add one more (should be dropped)
	req := createTestRequest("overflow-user")
	err := q.Enqueue(ctx, req)
	assert.Equal(t, domain.ErrQueueFull, err)

	stats = q.Stats()
	assert.Equal(t, int64(1), stats.DroppedTotal)
}

func TestMemoryQueue_HealthyUtilization(t *testing.T) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(10, time.Second, logger)
	defer q.Close()

	ctx := context.Background()

	// Test different utilization levels
	testCases := []struct {
		items     int
		healthy   bool
		utilization float64
	}{
		{0, true, 0.0},    // Empty
		{5, true, 50.0},   // Half full
		{8, true, 80.0},   // High but healthy
		{9, false, 90.0},  // At threshold (unhealthy)
		{10, false, 100.0}, // Full (unhealthy)
	}

	for _, tc := range testCases {
		// Reset queue
		q.Close()
		q = queue.NewMemoryQueue(10, time.Second, logger)
		
		// Add items
		for i := 0; i < tc.items; i++ {
			req := createTestRequest(fmt.Sprintf("user-%d", i))
			q.Enqueue(ctx, req)
		}

		stats := q.Stats()
		assert.Equal(t, tc.healthy, stats.IsHealthy(), "Items: %d", tc.items)
		assert.Equal(t, tc.utilization, stats.UtilizationPercent(), "Items: %d", tc.items)
	}
}

func BenchmarkMemoryQueue_EnqueueDequeue(b *testing.B) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(1000, time.Second, logger)
	defer q.Close()

	ctx := context.Background()
	req := createTestRequest("bench-user")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Enqueue
			err := q.Enqueue(ctx, req)
			if err != nil {
				b.Fatal(err)
			}

			// Dequeue
			_, err = q.Dequeue(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkMemoryQueue_EnqueueOnly(b *testing.B) {
	logger := testutils.NewMockLogger()
	q := queue.NewMemoryQueue(b.N+1000, time.Second, logger) // Size based on N
	defer q.Close()

	ctx := context.Background()
	req := createTestRequest("bench-user")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := q.Enqueue(ctx, req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

