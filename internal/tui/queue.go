package tui

import (
	"context"
	"sync"
)

type Queue struct {
	mu       sync.Mutex
	items    []Event
	capacity int
	notify   chan struct{}
	closed   bool
}

func NewQueue(capacity int) *Queue {
	if capacity < 8 {
		capacity = 8
	}
	return &Queue{capacity: capacity, notify: make(chan struct{}, 1)}
}

func (q *Queue) Push(event Event) bool {
	if !event.Valid() {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	if event.Kind == TurnProgress && len(q.items) > 0 && q.items[len(q.items)-1].Kind == TurnProgress {
		q.items[len(q.items)-1] = event
		return true
	}
	if len(q.items) >= q.capacity {
		for index, item := range q.items {
			if item.Kind == TurnProgress {
				copy(q.items[index:], q.items[index+1:])
				q.items = q.items[:len(q.items)-1]
				break
			}
		}
	}
	if len(q.items) >= q.capacity {
		return false
	}
	q.items = append(q.items, event)
	select {
	case q.notify <- struct{}{}:
	default:
	}
	return true
}

func (q *Queue) Pop(ctx context.Context) (Event, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			event := q.items[0]
			q.items[0] = Event{}
			q.items = q.items[1:]
			q.mu.Unlock()
			return event, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return Event{}, false
		}
		select {
		case <-ctx.Done():
			return Event{}, false
		case <-q.notify:
		}
	}
}
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}
func (q *Queue) Len() int { q.mu.Lock(); defer q.mu.Unlock(); return len(q.items) }
