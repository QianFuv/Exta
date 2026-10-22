package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueueBoundsCancellationAndRelease(t *testing.T) {
	queue := &Queue{}
	release, err := queue.Acquire(t.Context(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() {
		release, err := queue.Acquire(ctx, 1, 1)
		if release != nil {
			release()
		}
		finished <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for {
		_, waiting := queue.Counts()
		if waiting == 1 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("waiter not queued")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := queue.Acquire(t.Context(), 1, 1); err == nil {
		t.Fatal("queue admitted beyond its bound")
	}
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	release()
	release()
	if active, waiting := queue.Counts(); active != 0 || waiting != 0 {
		t.Fatalf("leaked capacity: %d/%d", active, waiting)
	}
	next, err := queue.Acquire(t.Context(), 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestQueueHandsOffWithoutExceedingConcurrency(t *testing.T) {
	queue := &Queue{}
	release, err := queue.Acquire(t.Context(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan func(), 1)
	go func() {
		next, err := queue.Acquire(t.Context(), 1, 1)
		if err == nil {
			entered <- next
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		_, waiting := queue.Counts()
		if waiting == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter did not queue")
		}
		time.Sleep(time.Millisecond)
	}
	release()
	select {
	case next := <-entered:
		if active, _ := queue.Counts(); active != 1 {
			t.Fatalf("active=%d", active)
		}
		next()
	case <-time.After(time.Second):
		t.Fatal("waiter did not acquire")
	}
}
