package initialize

import "context"

// Scrypt itself cannot be interrupted. Bound outstanding recovery verification
// to one operation, including after its caller cancels. Waiting callers can
// cancel without allocating a password copy or starting another KDF.
var recoveryPasswordSlot = make(chan struct{}, 1)

func verifyRecoveryPassword(ctx context.Context, password []byte, verify func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case recoveryPasswordSlot <- struct{}{}:
	}
	if err := ctx.Err(); err != nil {
		<-recoveryPasswordSlot
		return err
	}
	owned := append([]byte(nil), password...)
	done := make(chan error, 1)
	go func() {
		// This worker performs only the unchanged cryptographic check. It has
		// no stores, callbacks into recovery, or authority to continue effects.
		err := verify(owned)
		clear(owned)
		<-recoveryPasswordSlot
		done <- err
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		// Never admit persistence on a completed but expired verification.
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
}
