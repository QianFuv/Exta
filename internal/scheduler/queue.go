// Package scheduler bounds synchronous work before any upstream admission.
package scheduler

import (
	"context"
	"sync"

	"exta/internal/store"
)

type waiter struct {
	ready    chan struct{}
	limit    int
	admitted bool
}

// Queue is a process-local FIFO; durable jobs use the separate SQLite queue.
type Queue struct {
	mutex   sync.Mutex
	active  int
	waiting []*waiter
}

// Acquire waits within the caller's deadline and returns an idempotent release.
// Waiting never consumes budget or claims an upstream slot.
func (queue *Queue) Acquire(ctx context.Context, limit, waiting int) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	queue.mutex.Lock()
	if limit < 1 || waiting < 0 {
		queue.mutex.Unlock()
		return nil, store.Reject("capacity_unavailable", 503, "Synchronous admission is disabled")
	}
	if queue.active < limit && len(queue.waiting) == 0 {
		queue.active++
		queue.mutex.Unlock()
		return queue.release(), nil
	}
	if len(queue.waiting) >= waiting {
		queue.mutex.Unlock()
		return nil, store.Reject("capacity_unavailable", 503, "Synchronous queue is full")
	}
	ticket := &waiter{ready: make(chan struct{}), limit: limit}
	queue.waiting = append(queue.waiting, ticket)
	queue.mutex.Unlock()
	select {
	case <-ticket.ready:
		if err := ctx.Err(); err != nil {
			queue.release()()
			return nil, err
		}
		return queue.release(), nil
	case <-ctx.Done():
		queue.mutex.Lock()
		if ticket.admitted {
			queue.active--
		} else {
			for index, candidate := range queue.waiting {
				if candidate == ticket {
					queue.waiting = append(queue.waiting[:index], queue.waiting[index+1:]...)
					break
				}
			}
		}
		queue.wake()
		queue.mutex.Unlock()
		return nil, ctx.Err()
	}
}

func (queue *Queue) release() func() {
	var once sync.Once
	return func() {
		once.Do(func() { queue.mutex.Lock(); defer queue.mutex.Unlock(); queue.active--; queue.wake() })
	}
}

func (queue *Queue) wake() {
	for len(queue.waiting) > 0 && queue.active < queue.waiting[0].limit {
		ticket := queue.waiting[0]
		queue.waiting = queue.waiting[1:]
		queue.active++
		ticket.admitted = true
		close(ticket.ready)
	}
}

// Counts returns a consistent management snapshot without changing admission.
func (queue *Queue) Counts() (int, int) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	return queue.active, len(queue.waiting)
}
