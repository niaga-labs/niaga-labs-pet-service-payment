package rail_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Kilat-Pet-Delivery/service-payment/internal/rail"
)

// fakeClock is a hand-rolled test clock that captures scheduled functions and
// lets tests advance simulated time to trigger them synchronously.
type fakeClock struct {
	now     time.Duration
	pending []fakeTimer
}

type fakeTimer struct {
	fireAt time.Duration
	fn     func()
}

func (fc *fakeClock) AfterFunc(d time.Duration, f func()) {
	fc.pending = append(fc.pending, fakeTimer{fireAt: fc.now + d, fn: f})
}

// Advance moves the fake clock forward by d and runs all functions whose fire
// time is now <= the new clock position, in schedule order.
func (fc *fakeClock) Advance(d time.Duration) {
	fc.now += d
	for i, t := range fc.pending {
		if t.fn != nil && fc.now >= t.fireAt {
			t.fn()
			fc.pending[i].fn = nil // mark as fired
		}
	}
}

// newTestRail returns a SimulatedRail wired with a nop logger and the supplied
// fake clock so the test controls time completely.
func newTestRail(delay time.Duration, fc *fakeClock) *rail.SimulatedRail {
	logger := zap.NewNop()
	return rail.NewSimulatedRail(delay, logger, fc)
}

// ----------------------------------------------------------------------------

func TestSimulatedRail_Submit_ReturnsTxRef(t *testing.T) {
	fc := &fakeClock{}
	r := newTestRail(30*time.Second, fc)
	ctx := context.Background()

	runnerID := uuid.New()
	destID := uuid.New()

	txRef, err := r.Submit(ctx, runnerID, 5000, destID)
	require.NoError(t, err)
	assert.NotEmpty(t, txRef)
	assert.True(t, strings.HasPrefix(txRef, "DN-"), "expected txRef to start with DN-, got %q", txRef)

	status, err := r.Status(ctx, txRef)
	require.NoError(t, err)
	assert.Equal(t, rail.RailStatusProcessing, status, "immediately after Submit, status should be PROCESSING")
}

func TestSimulatedRail_CompletesAfterConfiguredDelay(t *testing.T) {
	fc := &fakeClock{}
	r := newTestRail(30*time.Second, fc)
	ctx := context.Background()

	txRef, err := r.Submit(ctx, uuid.New(), 1000, uuid.New())
	require.NoError(t, err)

	// Before the delay elapses the status must still be PROCESSING.
	status, err := r.Status(ctx, txRef)
	require.NoError(t, err)
	assert.Equal(t, rail.RailStatusProcessing, status)

	// 29 seconds elapsed — still processing.
	fc.Advance(29 * time.Second)
	status, err = r.Status(ctx, txRef)
	require.NoError(t, err)
	assert.Equal(t, rail.RailStatusProcessing, status, "status should be PROCESSING at t+29s")

	// 1 more second (total 30s) — the timer fires and status flips to COMPLETED.
	fc.Advance(1 * time.Second)
	status, err = r.Status(ctx, txRef)
	require.NoError(t, err)
	assert.Equal(t, rail.RailStatusCompleted, status, "status should be COMPLETED at t+30s")
}

func TestSimulatedRail_Status_UnknownRef_ReturnsError(t *testing.T) {
	fc := &fakeClock{}
	r := newTestRail(30*time.Second, fc)
	ctx := context.Background()

	_, err := r.Status(ctx, "DN-does-not-exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, rail.ErrRailRefNotFound)
}

func TestSimulatedRail_NeverFails(t *testing.T) {
	const N = 5
	fc := &fakeClock{}
	r := newTestRail(30*time.Second, fc)
	ctx := context.Background()

	refs := make([]string, N)
	for i := range refs {
		txRef, err := r.Submit(ctx, uuid.New(), int64((i+1)*100), uuid.New())
		require.NoError(t, err, "Submit #%d must not error", i)
		refs[i] = txRef
	}

	// Advance past the delay so all timers fire.
	fc.Advance(31 * time.Second)

	for i, txRef := range refs {
		status, err := r.Status(ctx, txRef)
		require.NoError(t, err, "Status for ref #%d must not error", i)
		assert.NotEqual(t, rail.RailStatusFailed, status, "simulated rail must never produce FAILED; ref #%d got %s", i, status)
		assert.Equal(t, rail.RailStatusCompleted, status, "ref #%d should be COMPLETED after delay", i)
	}
}
