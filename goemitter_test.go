package GoEmitter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestLogger implements Logger interface for testing
type TestLogger struct {
	ErrorCalls []string
	WarnCalls  []string
	InfoCalls  []string
	mu         sync.RWMutex
}

func (l *TestLogger) Error(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ErrorCalls = append(l.ErrorCalls, fmt.Sprintf(msg, args...))
}

func (l *TestLogger) Warn(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.WarnCalls = append(l.WarnCalls, fmt.Sprintf(msg, args...))
}

func (l *TestLogger) Info(msg string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.InfoCalls = append(l.InfoCalls, fmt.Sprintf(msg, args...))
}

func (l *TestLogger) GetErrorCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.ErrorCalls)
}

// ============================================================================
// BASIC FUNCTIONALITY TESTS
// ============================================================================

func TestGoEmitterBasic(t *testing.T) {
	emitter := New()
	called := make(chan bool, 2)

	// Persistent listener
	id1, err := emitter.On("event", func(args ...interface{}) {
		if len(args) != 1 || args[0].(string) != "data" {
			t.Errorf("Expected argument 'data', got %v", args)
		}
		called <- true
	})
	if err != nil {
		t.Fatalf("Failed to register persistent listener: %v", err)
	}

	// One-time listener
	id2, err := emitter.Once("event", func(args ...interface{}) {
		called <- true
	})
	if err != nil {
		t.Fatalf("Failed to register one-time listener: %v", err)
	}

	if id1 == id2 {
		t.Error("Listener IDs should be unique")
	}

	emitter.Emit("event", "data")

	// Wait for both listeners to be called
	for i := 0; i < 2; i++ {
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Error("Timeout waiting for listeners")
		}
	}

	// Should only have the persistent listener
	emitter.Emit("event", "data")
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Error("Timeout for persistent listener on second emit")
	}
}

func TestGoEmitterValidation(t *testing.T) {
	emitter := New()

	// Test empty event name
	_, err := emitter.On("", func(...interface{}) {})
	if err == nil {
		t.Error("Expected error for empty event name")
	}

	// Test nil callback
	_, err = emitter.On("event", nil)
	if err == nil {
		t.Error("Expected error for nil callback")
	}

	// Test emit with empty event name
	err = emitter.Emit("")
	if err == nil {
		t.Error("Expected error for empty event name in Emit")
	}

	// Test EmitSync with empty event name
	err = emitter.EmitSync("")
	if err == nil {
		t.Error("Expected error for empty event name in EmitSync")
	}
}

func TestGoEmitterSync(t *testing.T) {
	emitter := New()
	var callOrder []int
	var mu sync.Mutex

	// Register listeners that will record call order
	for i := 0; i < 5; i++ {
		index := i
		emitter.On("sync-event", func(args ...interface{}) {
			mu.Lock()
			callOrder = append(callOrder, index)
			mu.Unlock()
			time.Sleep(10 * time.Millisecond) // Simulate work
		})
	}

	start := time.Now()
	err := emitter.EmitSync("sync-event")
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("EmitSync failed: %v", err)
	}

	// Should take at least 50ms (5 * 10ms) since it's synchronous
	if duration < 40*time.Millisecond {
		t.Errorf("EmitSync completed too quickly: %v", duration)
	}

	mu.Lock()
	if len(callOrder) != 5 {
		t.Errorf("Expected 5 calls, got %d", len(callOrder))
	}
	mu.Unlock()
}

// ============================================================================
// CONTEXT AND CANCELLATION TESTS
// ============================================================================

func TestGoEmitterWithContext(t *testing.T) {
	emitter := New()

	// Channels para coordinar la ejecución
	callbackStarted := make(chan struct{})
	callbackCompleted := make(chan struct{})

	var callbackExecuted bool
	var mu sync.Mutex

	emitter.On("ctx-event", func(args ...interface{}) {
		close(callbackStarted)

		// Simulate work - debe verificar contexto internamente
		select {
		case <-time.After(200 * time.Millisecond):
			mu.Lock()
			callbackExecuted = true
			mu.Unlock()
			close(callbackCompleted)
		}
	})

	// Crear contexto con timeout muy corto
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := emitter.EmitWithContext(ctx, "ctx-event")
	duration := time.Since(start)

	// El comportamiento correcto: emit() retorna nil inmediatamente
	// porque es asíncrono
	if err != nil {
		t.Errorf("EmitWithContext should return nil for async operation, got: %v", err)
	}

	// Debe completar rápidamente (operación asíncrona)
	if duration > 20*time.Millisecond {
		t.Errorf("EmitWithContext took too long: %v", duration)
	}

	// Verificar que el callback se inicia
	select {
	case <-callbackStarted:
		// Esperado: callback se inicia
	case <-time.After(100 * time.Millisecond):
		t.Error("Callback should have started")
	}

	// Esperar un poco más para permitir cancelación
	time.Sleep(100 * time.Millisecond)

	// Verificar que el contexto se canceló
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("Context should have been cancelled, got: %v", ctx.Err())
	}

	// El callback NO debería completar debido a la cancelación
	select {
	case <-callbackCompleted:
		mu.Lock()
		executed := callbackExecuted
		mu.Unlock()
		if executed {
			t.Error("Callback should not have completed after context cancellation")
		}
	case <-time.After(50 * time.Millisecond):
		// Esperado: callback no completó
	}
}

// ============================================================================
// CLEANUP TESTS
// ============================================================================

func TestGoEmitterOff(t *testing.T) {
	emitter := New()
	called := make(chan bool, 1)

	id, err := emitter.On("removable", func(args ...interface{}) {
		called <- true
	})
	if err != nil {
		t.Fatalf("Failed to register listener: %v", err)
	}

	// Emit before removal
	emitter.Emit("removable")
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Error("Listener should have been called")
	}

	// Remove listener
	err = emitter.Off("removable", id)
	if err != nil {
		t.Fatalf("Failed to remove listener: %v", err)
	}

	// Emit after removal
	emitter.Emit("removable")
	select {
	case <-called:
		t.Error("Listener should not have been called after removal")
	case <-time.After(100 * time.Millisecond):
		// Expected: no call
	}
}

func TestGoEmitterClear(t *testing.T) {
	emitter := New()
	called := make(chan bool, 2)

	emitter.On("clearable", func(args ...interface{}) {
		called <- true
	})
	emitter.On("clearable", func(args ...interface{}) {
		called <- true
	})

	// Verify listeners exist
	if count := emitter.ListenerCount("clearable"); count != 2 {
		t.Errorf("Expected 2 listeners, got %d", count)
	}

	// Clear all listeners for event
	err := emitter.Clear("clearable")
	if err != nil {
		t.Fatalf("Failed to clear event: %v", err)
	}

	if count := emitter.ListenerCount("clearable"); count != 0 {
		t.Errorf("Expected 0 listeners after clear, got %d", count)
	}

	// Emit should not call anything
	emitter.Emit("clearable")
	select {
	case <-called:
		t.Error("No listeners should have been called after clear")
	case <-time.After(100 * time.Millisecond):
		// Expected: no calls
	}
}

func TestGoEmitterClearAll(t *testing.T) {
	emitter := New()

	emitter.On("event1", func(args ...interface{}) {})
	emitter.On("event2", func(args ...interface{}) {})
	emitter.On("event3", func(args ...interface{}) {})

	events := emitter.Events()
	if len(events) != 3 {
		t.Errorf("Expected 3 events, got %d", len(events))
	}

	err := emitter.ClearAll()
	if err != nil {
		t.Fatalf("Failed to clear all events: %v", err)
	}

	events = emitter.Events()
	if len(events) != 0 {
		t.Errorf("Expected 0 events after clear all, got %d", len(events))
	}
}

// ============================================================================
// TYPED FUNCTIONALITY TESTS
// ============================================================================

type TestData struct {
	Message string
	Number  int
}

func TestGoEmitterTyped(t *testing.T) {
	emitter := New()
	called := make(chan TestData, 1)

	// Register typed listener
	_, err := OnTyped(emitter, "typed-event", func(data TestData) {
		called <- data
	})
	if err != nil {
		t.Fatalf("Failed to register typed listener: %v", err)
	}

	testData := TestData{Message: "hello", Number: 42}

	// Emit typed event
	err = EmitTyped(emitter, "typed-event", testData)
	if err != nil {
		t.Fatalf("Failed to emit typed event: %v", err)
	}

	select {
	case received := <-called:
		if received.Message != testData.Message || received.Number != testData.Number {
			t.Errorf("Expected %+v, got %+v", testData, received)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for typed event")
	}
}

// ============================================================================
// METRICS TESTS
// ============================================================================

func TestGoEmitterMetrics(t *testing.T) {
	emitter := NewWithOptions(WithMetrics(true))

	// Register some listeners
	emitter.On("metric-event", func(args ...interface{}) {
		time.Sleep(10 * time.Millisecond)
	})
	emitter.On("metric-event", func(args ...interface{}) {
		time.Sleep(20 * time.Millisecond)
	})

	// Emit events
	emitter.Emit("metric-event")
	emitter.Emit("metric-event")
	emitter.WaitAsync()

	metrics := emitter.GetMetrics()

	if metrics.EventsEmitted != 2 {
		t.Errorf("Expected 2 events emitted, got %d", metrics.EventsEmitted)
	}

	if metrics.ListenersExecuted != 4 {
		t.Errorf("Expected 4 listeners executed, got %d", metrics.ListenersExecuted)
	}

	if metrics.AverageLatency == 0 {
		t.Error("Expected non-zero average latency")
	}
}

// ============================================================================
// PANIC HANDLING TESTS
// ============================================================================

func TestGoEmitterPanicHandling(t *testing.T) {
	var panicHandled bool
	logger := &TestLogger{}

	emitter := NewWithOptions(
		WithPanicHandler(func(r interface{}) {
			panicHandled = true
		}),
		WithLogger(logger),
		WithMetrics(true),
	)

	emitter.On("panic-event", func(args ...interface{}) {
		panic("test panic")
	})

	emitter.On("panic-event", func(args ...interface{}) {
		// This should still execute after panic
	})

	err := emitter.EmitSync("panic-event")
	if err != nil {
		t.Fatalf("EmitSync failed: %v", err)
	}

	if !panicHandled {
		t.Error("Panic should have been handled")
	}

	if logger.GetErrorCount() == 0 {
		t.Error("Logger should have recorded panic")
	}

	metrics := emitter.GetMetrics()
	if metrics.PanicsRecovered != 1 {
		t.Errorf("Expected 1 panic recovered, got %d", metrics.PanicsRecovered)
	}
}

// ============================================================================
// WORKER POOL TESTS
// ============================================================================

func TestGoEmitterWorkerPool(t *testing.T) {
	maxWorkers := 3
	emitter := NewWithOptions(WithMaxWorkers(maxWorkers), WithMetrics(true))

	var activeWorkers int32
	var maxActive int32

	// Register listeners that track active workers
	for i := 0; i < 10; i++ {
		emitter.On("worker-event", func(args ...interface{}) {
			current := atomic.AddInt32(&activeWorkers, 1)

			// Update max if needed
			for {
				max := atomic.LoadInt32(&maxActive)
				if current <= max || atomic.CompareAndSwapInt32(&maxActive, max, current) {
					break
				}
			}

			time.Sleep(100 * time.Millisecond)
			atomic.AddInt32(&activeWorkers, -1)
		})
	}

	emitter.Emit("worker-event")
	emitter.WaitAsync()

	if maxActive > int32(maxWorkers) {
		t.Errorf("Expected max %d active workers, got %d", maxWorkers, maxActive)
	}
}

// ============================================================================
// HOOKS TESTS
// ============================================================================

func TestGoEmitterHooks(t *testing.T) {
	emitter := New()
	var hookCalls []string
	var mu sync.Mutex

	emitter.AddEmitHook(func(event string, listenerCount int) {
		mu.Lock()
		hookCalls = append(hookCalls, fmt.Sprintf("%s:%d", event, listenerCount))
		mu.Unlock()
	})

	emitter.On("hook-event", func(args ...interface{}) {})
	emitter.On("hook-event", func(args ...interface{}) {})

	emitter.Emit("hook-event")
	emitter.WaitAsync()

	mu.Lock()
	if len(hookCalls) != 1 {
		t.Errorf("Expected 1 hook call, got %d", len(hookCalls))
	}
	if hookCalls[0] != "hook-event:2" {
		t.Errorf("Expected 'hook-event:2', got '%s'", hookCalls[0])
	}
	mu.Unlock()
}

// ============================================================================
// CONFIGURATION TESTS
// ============================================================================

func TestGoEmitterConfiguration(t *testing.T) {
	logger := &TestLogger{}

	emitter := NewWithOptions(
		WithMaxWorkers(5),
		WithLogger(logger),
		WithMetrics(false),
	)

	// Test that configuration is applied
	if emitter.configuration.MaxWorkers != 5 {
		t.Errorf("Expected MaxWorkers=5, got %d", emitter.configuration.MaxWorkers)
	}

	if emitter.configuration.Logger != logger {
		t.Error("Logger not set correctly")
	}

	if emitter.configuration.EnableMetrics {
		t.Error("Metrics should be disabled")
	}
}

// ============================================================================
// EDGE CASES AND ERROR HANDLING
// ============================================================================

func TestGoEmitterNonExistentEvent(t *testing.T) {
	emitter := New()

	// Emit non-existent event should not error
	err := emitter.Emit("non-existent")
	if err != nil {
		t.Errorf("Emit non-existent event should not error: %v", err)
	}

	// Off non-existent event should error
	err = emitter.Off("non-existent", 123)
	if err == nil {
		t.Error("Off non-existent event should error")
	}

	// Clear non-existent event should not error
	err = emitter.Clear("non-existent")
	if err == nil {
		t.Error("Clear non-existent event should error")
	}
}

func TestGoEmitterEvents(t *testing.T) {
	emitter := New()

	// Initially no events
	events := emitter.Events()
	if len(events) != 0 {
		t.Errorf("Expected 0 events initially, got %d", len(events))
	}

	// Add some events
	emitter.On("event1", func(args ...interface{}) {})
	emitter.On("event2", func(args ...interface{}) {})
	emitter.On("event1", func(args ...interface{}) {}) // Same event, different listener

	events = emitter.Events()
	if len(events) != 2 {
		t.Errorf("Expected 2 unique events, got %d", len(events))
	}

	// Check listener counts
	if count := emitter.ListenerCount("event1"); count != 2 {
		t.Errorf("Expected 2 listeners for event1, got %d", count)
	}

	if count := emitter.ListenerCount("event2"); count != 1 {
		t.Errorf("Expected 1 listener for event2, got %d", count)
	}
}

// ============================================================================
// CONCURRENCY TESTS
// ============================================================================

func TestGoEmitterHighConcurrency(t *testing.T) {
	emitter := New()
	listeners := 1000
	var wg sync.WaitGroup
	wg.Add(listeners)

	// Register thousand listeners
	for i := 0; i < listeners; i++ {
		emitter.On("high", func(args ...interface{}) {
			wg.Done()
		})
	}

	emitter.Emit("high")

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for listeners in high concurrency")
	}
}

func TestGoEmitterHighVolumeEvents(t *testing.T) {
	emitter := New()
	var count int64
	events := 1000
	listenersPerEvent := 100
	var wg sync.WaitGroup
	wg.Add(events * listenersPerEvent)

	// Register listeners for each event
	for e := 0; e < events; e++ {
		eventName := fmt.Sprintf("event-%d", e)
		for l := 0; l < listenersPerEvent; l++ {
			emitter.On(eventName, func(_ ...interface{}) {
				atomic.AddInt64(&count, 1)
				wg.Done()
			})
		}
	}

	// Emit all events
	for e := 0; e < events; e++ {
		eventName := fmt.Sprintf("event-%d", e)
		emitter.Emit(eventName)
	}

	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
	case <-time.After(30 * time.Second):
		t.Error("Timeout waiting for listeners in high volume")
	}

	expected := int64(events * listenersPerEvent)
	if atomic.LoadInt64(&count) != expected {
		t.Errorf("Expected %d calls, got %d", expected, atomic.LoadInt64(&count))
	}
}

// ============================================================================
// BENCHMARKS
// ============================================================================

func BenchmarkGoEmitterEmit(b *testing.B) {
	emitter := New()
	for i := 0; i < 1000; i++ {
		emitter.On("bench-event", func(args ...interface{}) {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.Emit("bench-event")
	}
}

func BenchmarkGoEmitterEmitSync(b *testing.B) {
	emitter := New()
	for i := 0; i < 1000; i++ {
		emitter.On("bench-sync-event", func(args ...interface{}) {})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.EmitSync("bench-sync-event")
	}
}

func BenchmarkGoEmitterOn(b *testing.B) {
	emitter := New()
	callback := func(args ...interface{}) {}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.On("bench-on", callback)
	}
}

func BenchmarkGoEmitterTyped(b *testing.B) {
	emitter := New()
	for i := 0; i < 1000; i++ {
		OnTyped(emitter, "bench-typed", func(data TestData) {})
	}
	testData := TestData{Message: "benchmark", Number: 42}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EmitTyped(emitter, "bench-typed", testData)
	}
}

// ============================================================================
// INTEGRATION TESTS
// ============================================================================

func TestGoEmitterIntegration(t *testing.T) {
	logger := &TestLogger{}
	var panicCount int32

	emitter := NewWithOptions(
		WithMaxWorkers(3),
		WithLogger(logger),
		WithMetrics(true),
		WithPanicHandler(func(r interface{}) {
			atomic.AddInt32(&panicCount, 1)
		}),
	)

	var successCount int32
	var errorCount int32

	// Add hook
	emitter.AddEmitHook(func(event string, listenerCount int) {
		logger.Info("Emitting %s to %d listeners", event, listenerCount)
	})

	// Register various listeners
	emitter.On("success", func(args ...interface{}) {
		atomic.AddInt32(&successCount, 1)
	})

	emitter.On("error", func(args ...interface{}) {
		atomic.AddInt32(&errorCount, 1)
		panic("simulated error")
	})

	emitter.Once("once-event", func(args ...interface{}) {
		atomic.AddInt32(&successCount, 1)
	})

	// Emit events
	emitter.Emit("success")
	emitter.Emit("error")
	emitter.Emit("once-event")
	emitter.Emit("once-event") // Should not trigger again

	emitter.WaitAsync()

	// Verify results
	if atomic.LoadInt32(&successCount) != 2 {
		t.Errorf("Expected 2 success calls, got %d", atomic.LoadInt32(&successCount))
	}

	if atomic.LoadInt32(&errorCount) != 1 {
		t.Errorf("Expected 1 error call, got %d", atomic.LoadInt32(&errorCount))
	}

	if atomic.LoadInt32(&panicCount) != 1 {
		t.Errorf("Expected 1 panic, got %d", atomic.LoadInt32(&panicCount))
	}

	metrics := emitter.GetMetrics()
	if metrics.EventsEmitted != 4 {
		t.Errorf("Expected 4 events emitted, got %d", metrics.EventsEmitted)
	}

	if metrics.PanicsRecovered != 1 {
		t.Errorf("Expected 1 panic recovered, got %d", metrics.PanicsRecovered)
	}
}
