// SPDX-License-Identifier: Apache-2.0

// Package stream is an in-process refresh-signal broker for the authenticated
// SSE endpoint. Subscribers receive only a signal to re-fetch authorized data;
// no object state, message body, or identity is carried on the stream.
package stream

import (
	"sync"

	"github.com/google/uuid"
)

// Broker fans out refresh signals to connected users.
type Broker struct {
	mu          sync.Mutex
	subscribers map[uuid.UUID]map[chan struct{}]struct{}
}

// NewBroker creates an empty broker.
func NewBroker() *Broker {
	return &Broker{subscribers: make(map[uuid.UUID]map[chan struct{}]struct{})}
}

// Subscribe registers a user and returns the signal channel plus an
// idempotent unsubscribe function.
func (b *Broker) Subscribe(userID uuid.UUID) (<-chan struct{}, func()) {
	channel := make(chan struct{}, 1)
	b.mu.Lock()
	if b.subscribers[userID] == nil {
		b.subscribers[userID] = make(map[chan struct{}]struct{})
	}
	b.subscribers[userID][channel] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			if channels, ok := b.subscribers[userID]; ok {
				delete(channels, channel)
				if len(channels) == 0 {
					delete(b.subscribers, userID)
				}
			}
			b.mu.Unlock()
		})
	}
	return channel, cancel
}

// Notify sends a non-blocking refresh signal to one user.
func (b *Broker) Notify(userID uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for channel := range b.subscribers[userID] {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

// NotifyMany signals a set of users, deduplicating input.
func (b *Broker) NotifyMany(userIDs []uuid.UUID) {
	seen := make(map[uuid.UUID]bool, len(userIDs))
	for _, userID := range userIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		b.Notify(userID)
	}
}
