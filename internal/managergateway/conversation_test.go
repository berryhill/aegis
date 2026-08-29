package managergateway

import (
	"context"
	"testing"
	"time"
)

func TestBindTurnContextCancelsWithRuntimeLifetime(t *testing.T) {
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	requestCtx, stopRequest := context.WithCancel(context.Background())
	turnCtx, release := bindTurnContext(requestCtx, runtimeCtx)
	defer release()

	stopRuntime()
	select {
	case <-turnCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("turn continued after runtime mandate cancellation")
	}
	stopRequest()
}

func TestBindTurnContextCancelsWithRequest(t *testing.T) {
	runtimeCtx, stopRuntime := context.WithCancel(context.Background())
	defer stopRuntime()
	requestCtx, stopRequest := context.WithCancel(context.Background())
	turnCtx, release := bindTurnContext(requestCtx, runtimeCtx)
	defer release()

	stopRequest()
	select {
	case <-turnCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("turn continued after request cancellation")
	}
}
