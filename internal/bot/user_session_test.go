package bot

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockMessageHandler captures execution order and can simulate blocking/panics
type mockMessageHandler struct {
	mu           sync.Mutex
	executionLog []string
	blockCh      chan struct{} // Close this to unblock processing
	waitCh       chan struct{} // Closed when processing starts (for synchronization)
}

func newMockMessageHandler() *mockMessageHandler {
	return &mockMessageHandler{
		executionLog: make([]string, 0),
		blockCh:      make(chan struct{}),
		waitCh:       make(chan struct{}),
	}
}

func (h *mockMessageHandler) HandleSessionMessage(ctx context.Context, session *UserSession, msg SessionMessage) {
	// Log the message text
	h.mu.Lock()
	h.executionLog = append(h.executionLog, msg.Text)
	h.mu.Unlock()

	// Special handling for panic test
	if msg.Text == "PANIC" {
		panic("simulated worker panic")
	}

	// Special handling for blocking test
	if msg.Text == "BLOCK" {
		if h.waitCh != nil {
			close(h.waitCh) // Signal we are running
		}
		<-h.blockCh // Wait until allowed to proceed
	}
}

func (h *mockMessageHandler) getLog() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]string, len(h.executionLog))
	copy(result, h.executionLog)
	return result
}

// createTestSession creates a session with a mock handler for testing
func createTestSession(id int64) (*UserSession, *mockMessageHandler) {
	ctx, cancel := context.WithCancel(context.Background())
	handler := newMockMessageHandler()
	// Unblock by default for simple tests
	close(handler.blockCh)

	s := &UserSession{
		userId:  id,
		inbox:   make(chan SessionMessage, 10),
		ctx:     ctx,
		cancel:  cancel,
		handler: handler,
	}
	s.StartWorker()
	return s, handler
}

func TestWorker_SequentialProcessing(t *testing.T) {
	session, handler := createTestSession(123)
	defer session.Stop()

	// Send 3 messages asynchronously
	msgs := []string{"msg1", "msg2", "msg3"}
	for _, txt := range msgs {
		session.Send(SessionMessage{Text: txt})
	}

	// Use SendSync as a barrier to ensure previous async messages are done
	session.SendSync(SessionMessage{Text: "barrier"})

	log := handler.getLog()

	// Verify exact order preserved
	assert.Equal(t, []string{"msg1", "msg2", "msg3", "barrier"}, log)
}

func TestWorker_PanicRecovery(t *testing.T) {
	session, handler := createTestSession(123)
	defer session.Stop()

	// Send message that panics
	session.SendSync(SessionMessage{Text: "PANIC"})

	// Send normal message immediately after - worker should still be alive
	session.SendSync(SessionMessage{Text: "recovery"})

	log := handler.getLog()

	// Verify worker survived and processed both messages
	assert.Contains(t, log, "PANIC")
	assert.Contains(t, log, "recovery")
	assert.Equal(t, 2, len(log), "both messages should be logged")
}

func TestWorker_ConcurrentUsers(t *testing.T) {
	// Setup Session A (will be blocked)
	ctxA, cancelA := context.WithCancel(context.Background())
	handlerA := newMockMessageHandler()
	// Don't close blockCh - we want manual control
	handlerA.blockCh = make(chan struct{})
	handlerA.waitCh = make(chan struct{})

	sessionA := &UserSession{
		userId:  1,
		inbox:   make(chan SessionMessage, 10),
		ctx:     ctxA,
		cancel:  cancelA,
		handler: handlerA,
	}
	sessionA.StartWorker()
	defer sessionA.Stop()

	// Setup Session B (fast, unblocked)
	sessionB, handlerB := createTestSession(2)
	defer sessionB.Stop()

	// Start blocking message on Session A (in goroutine since SendSync will block)
	go sessionA.SendSync(SessionMessage{Text: "BLOCK"})

	// Wait for A to start processing and get stuck
	select {
	case <-handlerA.waitCh:
		// A is now blocked in handler
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Session A did not start processing")
	}

	// Send to Session B - should process immediately despite A being blocked
	sessionB.SendSync(SessionMessage{Text: "fast"})

	// Verify B finished while A is still blocked
	logB := handlerB.getLog()
	assert.Contains(t, logB, "fast", "Session B should complete while A is blocked")

	// Verify A is still blocked (only started, not finished with additional messages)
	logA := handlerA.getLog()
	assert.Equal(t, []string{"BLOCK"}, logA, "Session A should only have the blocking message")

	// Unblock A to allow cleanup
	close(handlerA.blockCh)
}

func TestWorker_GracefulShutdown_NoPendingMessages(t *testing.T) {
	session, handler := createTestSession(123)

	// Process some messages first
	session.SendSync(SessionMessage{Text: "msg1"})
	session.SendSync(SessionMessage{Text: "msg2"})

	// Stop should return quickly
	done := make(chan struct{})
	go func() {
		session.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Stop() timed out")
	}

	log := handler.getLog()
	assert.Equal(t, []string{"msg1", "msg2"}, log)
}

func TestWorker_GracefulShutdown_DrainsQueueWithoutDeadlock(t *testing.T) {
	// Create session but with a handler that we won't unblock
	ctx, cancel := context.WithCancel(context.Background())
	handler := newMockMessageHandler()
	// Don't close blockCh - messages would block if processed

	session := &UserSession{
		userId:  999,
		inbox:   make(chan SessionMessage, 10),
		ctx:     ctx,
		cancel:  cancel,
		handler: handler,
	}
	session.StartWorker()

	// Queue up messages with Done channels (simulating SendSync callers waiting)
	for i := 0; i < 5; i++ {
		done := make(chan struct{})
		session.inbox <- SessionMessage{Text: "pending", Done: done}
	}

	// Stop should not deadlock - it should cancel and drain
	stopDone := make(chan struct{})
	go func() {
		session.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		// Success: Stop returned without deadlock
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() timed out - potential deadlock")
	}
}

func TestWorker_SendSync_WaitsForCompletion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	handler := newMockMessageHandler()
	// Don't close - we control when processing completes
	handler.blockCh = make(chan struct{})
	handler.waitCh = make(chan struct{})

	session := &UserSession{
		userId:  123,
		inbox:   make(chan SessionMessage, 10),
		ctx:     ctx,
		cancel:  cancel,
		handler: handler,
	}
	session.StartWorker()
	defer session.Stop()

	// Start SendSync in goroutine
	sendDone := make(chan struct{})
	go func() {
		session.SendSync(SessionMessage{Text: "BLOCK"})
		close(sendDone)
	}()

	// Wait for handler to start
	select {
	case <-handler.waitCh:
		// Handler is now processing
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Handler did not start")
	}

	// Verify SendSync is still waiting
	select {
	case <-sendDone:
		t.Fatal("SendSync returned before handler completed")
	case <-time.After(50 * time.Millisecond):
		// Good - still waiting
	}

	// Unblock handler
	close(handler.blockCh)

	// Now SendSync should complete
	select {
	case <-sendDone:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SendSync did not return after handler completed")
	}
}
