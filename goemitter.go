// Package goemitter provides a concurrent-safe, feature-rich event emitter for Go.
// It supports both synchronous and asynchronous event handling, worker pools,
// metrics collection, and type-safe operations through generic methods.
//
// Basic Usage:
//
//	emitter := goemitter.New()
//
//	// Register a listener
//	id, err := emitter.On("user.created", func(args ...interface{}) {
//		fmt.Printf("User created: %v\n", args[0])
//	})
//
//	// Emit an event
//	err = emitter.Emit("user.created", "John Doe")
//
//	// Wait for all async callbacks to complete
//	emitter.WaitAsync()
//
// Advanced Usage with Type Safety:
//
//	type User struct {
//		Name string
//		Age  int
//	}
//
//	// Type-safe listener registration
//	id, err := goemitter.OnTyped(emitter, "user.created", func(user User) {
//		fmt.Printf("User: %s, Age: %d\n", user.Name, user.Age)
//	})
//
//	// Type-safe event emission
//	err = goemitter.EmitTyped(emitter, "user.created", User{Name: "Jane", Age: 25})
//
// Configuration Options:
//
//	emitter := goemitter.NewWithOptions(
//		goemitter.WithMaxWorkers(10),
//		goemitter.WithMetrics(true),
//		goemitter.WithPanicHandler(func(err interface{}) {
//			log.Printf("Panic recovered: %v", err)
//		}),
//	)
package GoEmitter

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// GEmitter defines the interface for event emitters without generics for backward compatibility.
// This interface provides the core event emitter functionality that works with any Go version.
// For type-safe operations, use the concrete GoEmitter type with generic helper functions.
type GEmitter interface {
	// Basic event operations
	On(event string, callback func(...interface{})) (uint64, error)
	Once(event string, callback func(...interface{})) (uint64, error)
	Emit(event string, args ...interface{}) error
	EmitSync(event string, args ...interface{}) error
	Off(event string, listenerID uint64) error
	Clear(event string) error
	ClearAll() error

	// Information and introspection
	ListenerCount(event string) int
	Events() []string

	// Advanced features
	EmitWithContext(ctx context.Context, event string, args ...interface{}) error
	WaitAsync()
	GetMetrics() EmitterMetrics
	AddEmitHook(hook EmitHook)
}

// TypedEmitter extends GEmitter with type-safe operations.
// Generic methods are available through helper functions that work with the concrete implementation.
type TypedEmitter interface {
	GEmitter
	// Type-safe methods are implemented as package-level generic functions
	// that accept *GoEmitter as their first parameter
}

// EmitterConfig holds configuration options for the emitter.
// Use DefaultConfig() to get sensible defaults, then modify as needed.
type EmitterConfig struct {
	MaxWorkers    int                              // Maximum concurrent workers (0 = unlimited)
	PanicHandler  func(recoveredPanic interface{}) // Handler for panics in callbacks
	Logger        Logger                           // Logger interface for error reporting
	BufferSize    int                              // Buffer size for internal channels
	EnableMetrics bool                             // Enable performance metrics collection
}

// Logger interface for pluggable logging support.
// Implement this interface to provide custom logging for the emitter.
// The emitter will log errors, warnings, and informational messages through this interface.
type Logger interface {
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Info(msg string, args ...interface{})
}

// EmitterMetrics contains real-time performance and usage metrics.
// Metrics are updated atomically when EnableMetrics is true in configuration.
// All fields use atomic operations for thread-safe access.
type EmitterMetrics struct {
	EventsEmitted     uint64 // Total number of events emitted
	ListenersExecuted uint64 // Total number of listeners executed
	PanicsRecovered   uint64 // Total number of panics recovered in callbacks
	AverageLatency    int64  // Average callback execution time in nanoseconds
	ActiveWorkers     int32  // Current number of active worker goroutines
}

// GetAverageLatencyDuration returns the average latency as a time.Duration.
// This is a convenience method to convert the atomic int64 nanoseconds to Duration.
func (m EmitterMetrics) GetAverageLatencyDuration() time.Duration {
	return time.Duration(m.AverageLatency)
}

// EmitHook is a function called before each emit operation.
// Use this to implement custom logic such as logging, debugging, or validation
// before events are dispatched to listeners.
type EmitHook func(event string, listenerCount int)

// eventListener stores information about each registered event listener.
// This is an internal structure used to manage individual event listeners.
type eventListener struct {
	id         uint64               // Unique identifier for the listener
	callback   func(...interface{}) // The callback function to execute
	persistent bool                 // Whether the listener persists after execution
	createdAt  time.Time            // Timestamp when the listener was created
}

// GoEmitter is a concurrent-safe, feature-rich event emitter implementation.
// It provides all event emitter functionality including type-safe operations
// through generic helper functions, worker pools, metrics collection, and more.
//
// Thread Safety:
// All operations on GoEmitter are thread-safe and can be called concurrently
// from multiple goroutines without additional synchronization.
//
// Performance:
// The emitter uses sync.Map for event storage and atomic operations for metrics
// to provide excellent performance under high concurrency scenarios.
type GoEmitter struct {
	listenerIDCounter uint64          // Atomic counter for generating unique listener IDs
	eventMap          sync.Map        // Thread-safe map storing events and their listeners
	asyncWaitGroup    sync.WaitGroup  // WaitGroup for tracking async callback execution
	configuration     EmitterConfig   // Configuration options for the emitter
	workerPool        chan struct{}   // Channel-based worker pool for limiting concurrency
	metrics           *EmitterMetrics // Performance metrics (accessed atomically)
	emitHooks         []EmitHook      // Hooks executed before each emit operation
	hooksMutex        sync.RWMutex    // Mutex protecting the hooks slice
}

// DefaultConfig returns a sensible default configuration for most use cases.
//
// Default values:
//   - MaxWorkers: 0 (unlimited concurrent workers)
//   - BufferSize: 1000 (internal channel buffer size)
//   - EnableMetrics: true (metrics collection enabled)
//   - PanicHandler: nil (panics are logged but not handled)
//   - Logger: nil (no logging)
func DefaultConfig() EmitterConfig {
	return EmitterConfig{
		MaxWorkers:    0,    // Unlimited workers
		BufferSize:    1000, // Reasonable buffer size
		EnableMetrics: true, // Enable metrics by default
	}
}

// EmitterOption represents a configuration option function.
// Use these with NewWithOptions to customize emitter behavior.
type EmitterOption func(*EmitterConfig)

// WithMaxWorkers sets the maximum number of concurrent worker goroutines.
// Setting to 0 (default) allows unlimited workers.
// Positive values create a worker pool that limits concurrency.
//
// Example:
//
//	emitter := goemitter.NewWithOptions(goemitter.WithMaxWorkers(10))
func WithMaxWorkers(maxWorkers int) EmitterOption {
	return func(config *EmitterConfig) {
		config.MaxWorkers = maxWorkers
	}
}

// WithPanicHandler sets a custom panic handler for callback execution.
// This handler will be called when a callback panics, allowing for graceful recovery.
// If not set, panics are logged through the Logger interface if available.
//
// Example:
//
//	emitter := goemitter.NewWithOptions(
//	    goemitter.WithPanicHandler(func(err interface{}) {
//	        log.Printf("Callback panic: %v", err)
//	    }),
//	)
func WithPanicHandler(handler func(interface{})) EmitterOption {
	return func(config *EmitterConfig) {
		config.PanicHandler = handler
	}
}

// WithLogger sets a custom logger for error and warning messages.
// Provide a Logger implementation to receive detailed information about
// emitter operations, errors, and recovered panics.
//
// Example:
//
//	emitter := goemitter.NewWithOptions(goemitter.WithLogger(myLogger))
func WithLogger(logger Logger) EmitterOption {
	return func(config *EmitterConfig) {
		config.Logger = logger
	}
}

// WithMetrics enables or disables performance metrics collection.
// Metrics collection has minimal overhead but can be disabled for maximum performance.
// When disabled, GetMetrics() will return zero values.
//
// Example:
//
//	emitter := goemitter.NewWithOptions(goemitter.WithMetrics(false))
func WithMetrics(enabled bool) EmitterOption {
	return func(config *EmitterConfig) {
		config.EnableMetrics = enabled
	}
}

// New creates a new GoEmitter with default configuration.
// This is equivalent to calling NewWithOptions() with no options.
//
// Example:
//
//	emitter := goemitter.New()
func New() *GoEmitter {
	return NewWithOptions()
}

// NewWithOptions creates a new GoEmitter with custom configuration options.
// Pass EmitterOption functions to customize the emitter's behavior.
//
// Example:
//
//	emitter := goemitter.NewWithOptions(
//	    goemitter.WithMaxWorkers(5),
//	    goemitter.WithMetrics(true),
//	    goemitter.WithPanicHandler(myPanicHandler),
//	)
func NewWithOptions(options ...EmitterOption) *GoEmitter {
	config := DefaultConfig()

	// Apply all provided options
	for _, option := range options {
		option(&config)
	}

	emitter := &GoEmitter{
		configuration: config,
		metrics:       &EmitterMetrics{},
		emitHooks:     make([]EmitHook, 0),
	}

	// Initialize worker pool if max workers is specified
	if config.MaxWorkers > 0 {
		emitter.workerPool = make(chan struct{}, config.MaxWorkers)
	}

	return emitter
}

// ============================================================================
// GENERIC HELPER FUNCTIONS - Type-safe operations
// ============================================================================

// OnTyped registers a type-safe persistent listener for the given event.
// This function is only available with concrete GoEmitter instances and provides
// compile-time type safety by accepting a callback that takes a specific type T.
//
// The listener will remain active until explicitly removed with Off() or Clear().
// Returns a unique listener ID that can be used to remove the listener later.
//
// Example:
//
//	type User struct { Name string }
//	id, err := goemitter.OnTyped(emitter, "user.created", func(user User) {
//	    fmt.Printf("User created: %s\n", user.Name)
//	})
func OnTyped[T any](emitter *GoEmitter, event string, callback func(T)) (uint64, error) {
	if event == "" {
		return 0, fmt.Errorf("event name cannot be empty")
	}
	if callback == nil {
		return 0, fmt.Errorf("callback cannot be nil")
	}

	// Create a wrapper that performs type assertion
	wrapper := func(args ...interface{}) {
		if len(args) > 0 {
			if typedArg, ok := args[0].(T); ok {
				callback(typedArg)
			}
		}
	}

	return emitter.addListener(event, wrapper, true), nil
}

// OnceTyped registers a type-safe one-time listener for the given event.
// This function provides compile-time type safety and automatically removes
// the listener after the first time it's triggered.
//
// Example:
//
//	id, err := goemitter.OnceTyped(emitter, "app.started", func(config AppConfig) {
//	    fmt.Printf("App started with config: %+v\n", config)
//	})
func OnceTyped[T any](emitter *GoEmitter, event string, callback func(T)) (uint64, error) {
	if event == "" {
		return 0, fmt.Errorf("event name cannot be empty")
	}
	if callback == nil {
		return 0, fmt.Errorf("callback cannot be nil")
	}

	// Create a wrapper that performs type assertion
	wrapper := func(args ...interface{}) {
		if len(args) > 0 {
			if typedArg, ok := args[0].(T); ok {
				callback(typedArg)
			}
		}
	}

	return emitter.addListener(event, wrapper, false), nil
}

// EmitTyped emits a type-safe event with the specified data.
// This function provides compile-time type safety by ensuring the data parameter
// matches the expected type T for the event.
//
// Example:
//
//	user := User{Name: "John"}
//	err := goemitter.EmitTyped(emitter, "user.created", user)
func EmitTyped[T any](emitter *GoEmitter, event string, data T) error {
	return emitter.Emit(event, data)
}

// EmitTypedWithContext emits a type-safe event with context for cancellation.
// Combines type safety with context-based cancellation support.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	err := goemitter.EmitTypedWithContext(ctx, emitter, "user.created", user)
func EmitTypedWithContext[T any](ctx context.Context, emitter *GoEmitter, event string, data T) error {
	return emitter.EmitWithContext(ctx, event, data)
}

// ============================================================================
// INTERFACE IMPLEMENTATION - Core event operations
// ============================================================================

// On registers a persistent listener for the given event.
// The listener will remain active until explicitly removed with Off() or Clear().
// Returns a unique listener ID that can be used to remove the listener later.
//
// Example:
//
//	id, err := emitter.On("user.created", func(args ...interface{}) {
//	    fmt.Printf("User created: %v\n", args[0])
//	})
func (e *GoEmitter) On(event string, callback func(...interface{})) (uint64, error) {
	if err := e.validateEventAndCallback(event, callback); err != nil {
		return 0, err
	}
	return e.addListener(event, callback, true), nil
}

// Once registers a one-time listener for the given event.
// The listener will automatically be removed after the first time it's triggered.
// Returns a unique listener ID for reference.
//
// Example:
//
//	id, err := emitter.Once("app.shutdown", func(args ...interface{}) {
//	    fmt.Println("Application shutting down")
//	})
func (e *GoEmitter) Once(event string, callback func(...interface{})) (uint64, error) {
	if err := e.validateEventAndCallback(event, callback); err != nil {
		return 0, err
	}
	return e.addListener(event, callback, false), nil
}

// addListener is the internal method used by On() and Once() to register listeners.
// It handles the atomic ID generation and thread-safe storage of listener information.
func (e *GoEmitter) addListener(event string, callback func(...interface{}), persistent bool) uint64 {
	// Generate unique listener ID atomically
	listenerID := atomic.AddUint64(&e.listenerIDCounter, 1)

	// Load or create the listeners map for this event
	eventListeners, _ := e.eventMap.LoadOrStore(event, &sync.Map{})
	listenersMap := eventListeners.(*sync.Map)

	// Store the listener
	listenersMap.Store(listenerID, eventListener{
		id:         listenerID,
		callback:   callback,
		persistent: persistent,
		createdAt:  time.Now(),
	})

	return listenerID
}

// Emit triggers all listeners for an event asynchronously.
// All listeners are executed concurrently in separate goroutines.
// Use WaitAsync() to wait for all callbacks to complete.
// Use EmitSync() if you need synchronous execution.
//
// Example:
//
//	err := emitter.Emit("user.created", "John Doe", 25)
func (e *GoEmitter) Emit(event string, args ...interface{}) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}

	return e.emitInternal(context.Background(), event, args...)
}

// EmitWithContext emits an event with context support for cancellation.
// Allows cancellation of pending listener executions through the provided context.
// This is useful for implementing timeouts or graceful shutdowns.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	err := emitter.EmitWithContext(ctx, "long.process", data)
func (e *GoEmitter) EmitWithContext(ctx context.Context, event string, args ...interface{}) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}

	return e.emitInternal(ctx, event, args...)
}

// emitInternal is the core emit implementation shared by Emit() and EmitWithContext().
// It handles context cancellation, hook execution, metrics updates, and listener dispatch.
func (e *GoEmitter) emitInternal(ctx context.Context, event string, args ...interface{}) error {
	// Check if event has any listeners
	eventListeners, exists := e.eventMap.Load(event)
	if !exists {
		return nil // No listeners registered for this event
	}

	listenersMap := eventListeners.(*sync.Map)
	listenerCount := e.countListenersInMap(listenersMap)

	// Execute registered hooks before dispatching to listeners
	e.executeEmitHooks(event, listenerCount)

	// Update metrics if enabled
	if e.configuration.EnableMetrics {
		atomic.AddUint64(&e.metrics.EventsEmitted, 1)
	}

	// Check for context cancellation before starting execution
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Dispatch to all listeners
	listenersMap.Range(func(key, value interface{}) bool {
		// Check for context cancellation on each listener
		select {
		case <-ctx.Done():
			return false // Stop iteration
		default:
		}

		listener := value.(eventListener)

		// Execute callback based on worker pool configuration
		if e.workerPool != nil {
			e.executeCallbackWithWorkerPool(ctx, listener.callback, args...)
		} else {
			e.executeCallbackAsync(ctx, listener.callback, args...)
		}

		// Remove one-time listeners after execution
		if !listener.persistent {
			listenersMap.Delete(key)
		}

		return true // Continue iteration
	})

	return ctx.Err()
}

// EmitSync emits an event synchronously.
// All listeners are executed sequentially in the calling goroutine.
// This method blocks until all listeners complete execution.
//
// Example:
//
//	err := emitter.EmitSync("user.created", "John Doe", 25)
func (e *GoEmitter) EmitSync(event string, args ...interface{}) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}

	// Check if event has any listeners
	eventListeners, exists := e.eventMap.Load(event)
	if !exists {
		return nil // No listeners registered for this event
	}

	listenersMap := eventListeners.(*sync.Map)
	listenerCount := e.countListenersInMap(listenersMap)

	// Execute registered hooks before dispatching to listeners
	e.executeEmitHooks(event, listenerCount)

	// Update metrics if enabled
	if e.configuration.EnableMetrics {
		atomic.AddUint64(&e.metrics.EventsEmitted, 1)
	}

	// Collect all listeners to execute
	var listenersToExecute []eventListener
	listenersMap.Range(func(_, value interface{}) bool {
		listenersToExecute = append(listenersToExecute, value.(eventListener))
		return true
	})

	// Execute callbacks synchronously
	for _, listener := range listenersToExecute {
		startTime := time.Now()
		e.executeCallbackWithPanicRecovery(listener.callback, args...)

		// Update latency metrics if enabled
		if e.configuration.EnableMetrics {
			executionDuration := time.Since(startTime)
			e.updateAverageLatencyMetrics(executionDuration)
		}

		// Remove one-time listeners after execution
		if !listener.persistent {
			listenersMap.Delete(listener.id)
		}
	}

	return nil
}

// executeCallbackWithWorkerPool executes a callback using the worker pool with context awareness.
// This method is used when MaxWorkers is configured to limit concurrency.
func (e *GoEmitter) executeCallbackWithWorkerPool(ctx context.Context, callback func(...interface{}), args ...interface{}) {
	// Check context before acquiring worker
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Acquire worker from pool (blocks if pool is full)
	e.workerPool <- struct{}{}
	e.asyncWaitGroup.Add(1)

	go func() {
		defer func() {
			<-e.workerPool // Release worker back to pool
			e.asyncWaitGroup.Done()
			atomic.AddInt32(&e.metrics.ActiveWorkers, -1)
		}()

		atomic.AddInt32(&e.metrics.ActiveWorkers, 1)

		// Check context again before executing
		select {
		case <-ctx.Done():
			return
		default:
			startTime := time.Now()
			e.executeCallbackWithPanicRecovery(callback, args...)

			// Update latency metrics if enabled
			if e.configuration.EnableMetrics {
				executionDuration := time.Since(startTime)
				e.updateAverageLatencyMetrics(executionDuration)
			}
		}
	}()
}

// executeCallbackAsync executes a callback asynchronously without worker pool constraints.
// This method is used when MaxWorkers is 0 (unlimited workers).
func (e *GoEmitter) executeCallbackAsync(ctx context.Context, callback func(...interface{}), args ...interface{}) {
	// Check context before starting goroutine
	select {
	case <-ctx.Done():
		return
	default:
	}

	e.asyncWaitGroup.Add(1)

	go func() {
		defer e.asyncWaitGroup.Done()

		// Check context before executing
		select {
		case <-ctx.Done():
			return
		default:
			startTime := time.Now()
			e.executeCallbackWithPanicRecovery(callback, args...)

			// Update latency metrics if enabled
			if e.configuration.EnableMetrics {
				executionDuration := time.Since(startTime)
				e.updateAverageLatencyMetrics(executionDuration)
			}
		}
	}()
}

// executeCallbackWithPanicRecovery executes a callback with panic recovery and metrics updates.
// This method handles panic recovery, logging, and metrics updates for all callback executions.
func (e *GoEmitter) executeCallbackWithPanicRecovery(callback func(...interface{}), args ...interface{}) {
	defer func() {
		if recoveredPanic := recover(); recoveredPanic != nil {
			// Update panic metrics if enabled
			if e.configuration.EnableMetrics {
				atomic.AddUint64(&e.metrics.PanicsRecovered, 1)
			}

			// Call custom panic handler if provided
			if e.configuration.PanicHandler != nil {
				e.configuration.PanicHandler(recoveredPanic)
			}

			// Log panic if logger is available
			if e.configuration.Logger != nil {
				e.configuration.Logger.Error("Panic recovered in callback: %v", recoveredPanic)
			}
		}
	}()

	// Execute the callback
	callback(args...)

	// Update execution metrics if enabled
	if e.configuration.EnableMetrics {
		atomic.AddUint64(&e.metrics.ListenersExecuted, 1)
	}
}

// Off removes a specific listener by its ID.
// Removes the listener with the given ID from the specified event.
// Returns an error if the event doesn't exist.
//
// Example:
//
//	err := emitter.Off("user.created", listenerID)
func (e *GoEmitter) Off(event string, listenerID uint64) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}

	eventListeners, exists := e.eventMap.Load(event)
	if !exists {
		return fmt.Errorf("event '%s' not found", event)
	}

	listenersMap := eventListeners.(*sync.Map)
	listenersMap.Delete(listenerID)
	return nil
}

// Clear removes all listeners for a specific event.
// Removes all listeners for the specified event and removes the event from the emitter.
// Returns an error if the event doesn't exist.
//
// Example:
//
//	err := emitter.Clear("user.created")
func (e *GoEmitter) Clear(event string) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}

	// Check if event exists
	_, exists := e.eventMap.Load(event)
	if !exists {
		return fmt.Errorf("event '%s' not found", event)
	}

	// Remove the entire event and all its listeners
	e.eventMap.Delete(event)
	return nil
}

// ClearAll removes all events and their listeners.
// Completely resets the emitter by removing all events and their listeners.
// This operation is safe to call concurrently and will not affect ongoing executions.
//
// Example:
//
//	err := emitter.ClearAll()
func (e *GoEmitter) ClearAll() error {
	e.eventMap.Range(func(key, _ interface{}) bool {
		e.eventMap.Delete(key)
		return true
	})
	return nil
}

// ListenerCount returns the number of listeners registered for a specific event.
// Returns 0 if the event doesn't exist or has no listeners.
//
// Example:
//
//	count := emitter.ListenerCount("user.created")
func (e *GoEmitter) ListenerCount(event string) int {
	eventListeners, exists := e.eventMap.Load(event)
	if !exists {
		return 0
	}

	listenersMap := eventListeners.(*sync.Map)
	return e.countListenersInMap(listenersMap)
}

// Events returns a slice of all registered event names.
// Returns all event names that have at least one listener.
// The order of events in the slice is not guaranteed.
//
// Example:
//
//	events := emitter.Events()
//	fmt.Printf("Active events: %v\n", events)
func (e *GoEmitter) Events() []string {
	var eventNames []string
	e.eventMap.Range(func(key, _ interface{}) bool {
		eventNames = append(eventNames, key.(string))
		return true
	})
	return eventNames
}

// WaitAsync blocks until all currently executing asynchronous callbacks complete.
// This method is useful for graceful shutdown or in tests where you need to ensure
// all async operations have finished before proceeding.
//
// Example:
//
//	emitter.Emit("user.created", userData)
//	emitter.WaitAsync() // Wait for all callbacks to complete
func (e *GoEmitter) WaitAsync() {
	e.asyncWaitGroup.Wait()
}

// GetMetrics returns a snapshot of current performance metrics.
// All metric values are loaded atomically, ensuring a consistent snapshot.
// If metrics are disabled, all values will be zero.
//
// Example:
//
//	metrics := emitter.GetMetrics()
//	fmt.Printf("Events emitted: %d\n", metrics.EventsEmitted)
//	fmt.Printf("Average latency: %v\n", metrics.GetAverageLatencyDuration())
func (e *GoEmitter) GetMetrics() EmitterMetrics {
	return EmitterMetrics{
		EventsEmitted:     atomic.LoadUint64(&e.metrics.EventsEmitted),
		ListenersExecuted: atomic.LoadUint64(&e.metrics.ListenersExecuted),
		PanicsRecovered:   atomic.LoadUint64(&e.metrics.PanicsRecovered),
		AverageLatency:    atomic.LoadInt64(&e.metrics.AverageLatency),
		ActiveWorkers:     atomic.LoadInt32(&e.metrics.ActiveWorkers),
	}
}

// AddEmitHook adds a hook function to be called before each emit operation.
// Hooks are executed in the order they were added, before any listeners are called.
// This is useful for implementing logging, debugging, validation, or other cross-cutting concerns.
//
// Example:
//
//	emitter.AddEmitHook(func(event string, listenerCount int) {
//	    log.Printf("Emitting event '%s' to %d listeners", event, listenerCount)
//	})
func (e *GoEmitter) AddEmitHook(hook EmitHook) {
	e.hooksMutex.Lock()
	defer e.hooksMutex.Unlock()
	e.emitHooks = append(e.emitHooks, hook)
}

// ============================================================================
// INTERNAL HELPER METHODS
// ============================================================================

// validateEventAndCallback validates common input parameters for event operations.
// This helper method is used by public methods to ensure consistent validation.
func (e *GoEmitter) validateEventAndCallback(event string, callback func(...interface{})) error {
	if event == "" {
		return fmt.Errorf("event name cannot be empty")
	}
	if callback == nil {
		return fmt.Errorf("callback cannot be nil")
	}
	return nil
}

// countListenersInMap counts the number of listeners in a sync.Map.
// This helper method provides a thread-safe way to count listeners.
func (e *GoEmitter) countListenersInMap(listenersMap *sync.Map) int {
	count := 0
	listenersMap.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// executeEmitHooks executes all registered emit hooks before event dispatch.
// This method is called internally before each emit operation to run custom hooks.
func (e *GoEmitter) executeEmitHooks(event string, listenerCount int) {
	e.hooksMutex.RLock()
	defer e.hooksMutex.RUnlock()

	for _, hook := range e.emitHooks {
		hook(event, listenerCount)
	}
}

// updateAverageLatencyMetrics updates the average latency metrics using atomic operations.
// This method uses a compare-and-swap loop to safely update the average latency
// in a concurrent environment without locks.
func (e *GoEmitter) updateAverageLatencyMetrics(executionDuration time.Duration) {
	// Convert duration to int64 nanoseconds for atomic operations
	newLatencyNanos := int64(executionDuration)

	// Use compare-and-swap loop to update average atomically
	for {
		currentAverage := atomic.LoadInt64(&e.metrics.AverageLatency)
		var newAverage int64

		if currentAverage == 0 {
			newAverage = newLatencyNanos
		} else {
			// Simple moving average calculation
			newAverage = (currentAverage + newLatencyNanos) / 2
		}

		// Attempt to update atomically
		if atomic.CompareAndSwapInt64(&e.metrics.AverageLatency, currentAverage, newAverage) {
			break
		}
		// If the compare-and-swap fails, retry with the new current value
	}
}
