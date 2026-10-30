package initialize

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/principalauth"
)

func TestRecoveryPasswordCancellationOwnsAndBoundsVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, finish, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var owned []byte
	defer func() {
		close(finish)
		// Acquiring the single slot joins completion of the owned check and
		// its password clearing, even if an earlier assertion fails.
		recoveryPasswordSlot <- struct{}{}
		<-recoveryPasswordSlot
		if !bytes.Equal(owned, make([]byte, len(owned))) {
			t.Error("cancelled verification copy was not cleared")
		}
	}()
	password := []byte("synthetic-recovery-password")
	go func() {
		result <- verifyRecoveryPassword(ctx, password, func(copy []byte) error {
			owned = copy
			close(entered)
			<-finish
			return principalauth.ErrAuthentication
		})
	}()
	<-entered
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation was not preserved: %T", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for noninterruptible verification")
	}
	clear(password)
	if !bytes.Equal(owned, []byte("synthetic-recovery-password")) {
		t.Fatal("cancelled caller and verification share password storage")
	}
	waiting, cancelWaiting := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	started := make(chan struct{})
	queuedContext := &recoveryWaitingContext{Context: waiting, waiting: started}
	go func() {
		queued <- verifyRecoveryPassword(queuedContext, nil, func([]byte) error {
			t.Error("queued cancelled call entered verification")
			return nil
		})
	}()
	<-started
	cancelWaiting()
	select {
	case err := <-queued:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled waiter did not preserve cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation waited for occupied slot")
	}
}

// Done is evaluated by the slot-admission select after the initial Err check.
// This barrier proves the queued caller passed that check with a live context
// while the first worker still owns the slot.
type recoveryWaitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *recoveryWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestRecoveryPasswordCancellationWinsCompletedResult(t *testing.T) {
	for _, result := range []error{nil, principalauth.ErrAuthentication} {
		ctx, cancel := context.WithCancel(context.Background())
		err := verifyRecoveryPassword(ctx, []byte("synthetic-recovery-password"), func([]byte) error {
			cancel()
			return result
		})
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal("completed verification admitted a cancelled caller")
		}
	}
}

func TestRecoveryPasswordPreservesAuthenticationResultAndClearsCopy(t *testing.T) {
	for _, expected := range []error{nil, principalauth.ErrAuthentication} {
		password := []byte("synthetic-recovery-password")
		var owned []byte
		err := verifyRecoveryPassword(context.Background(), password, func(copy []byte) error {
			owned = copy
			return expected
		})
		if !errors.Is(err, expected) {
			t.Fatal("verification result was suppressed")
		}
		if !bytes.Equal(owned, make([]byte, len(owned))) {
			t.Fatal("verification copy was not cleared")
		}
		if !bytes.Equal(password, []byte("synthetic-recovery-password")) {
			t.Fatal("verification modified caller storage")
		}
	}
}
