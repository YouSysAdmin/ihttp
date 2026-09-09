package eventbus

import (
	"context"
	"testing"
	"time"
)

func TestSubscribeReceivesAndClosesOnCancel(t *testing.T) {
	b := New()
	ctx, cancel := context.WithCancel(t.Context())
	ch := b.Subscribe(ctx)

	b.Emit("ping", 1)

	select {
	case e := <-ch:
		if e.Type != "ping" {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered")
	}

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected the channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after cancel")
	}
}

func TestPublishDoesNotBlockOnASlowSubscriber(t *testing.T) {
	b := New()
	_ = b.Subscribe(t.Context())

	done := make(chan struct{})
	go func() {
		for range subscriberBuffer * 2 {
			b.Emit("x", nil)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
}
