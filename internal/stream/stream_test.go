// SPDX-License-Identifier: Apache-2.0
package stream

import (
	"testing"

	"github.com/google/uuid"
)

func TestBrokerNotifiesSubscribedUserOnly(t *testing.T) {
	broker := NewBroker()
	left := uuid.New()
	right := uuid.New()
	leftChannel, unsubscribeLeft := broker.Subscribe(left)
	rightChannel, unsubscribeRight := broker.Subscribe(right)
	defer unsubscribeLeft()
	defer unsubscribeRight()

	broker.Notify(left)
	select {
	case <-leftChannel:
	default:
		t.Fatal("left subscriber did not receive a signal")
	}
	select {
	case <-rightChannel:
		t.Fatal("right subscriber received another user's signal")
	default:
	}

	// The channel is buffered and non-blocking; repeated signals collapse.
	broker.Notify(left)
	broker.Notify(left)
	select {
	case <-leftChannel:
	default:
		t.Fatal("no collapse signal")
	}
}

func TestBrokerUnsubscribeStopsSignals(t *testing.T) {
	broker := NewBroker()
	userID := uuid.New()
	channel, unsubscribe := broker.Subscribe(userID)
	unsubscribe()
	unsubscribe()
	broker.Notify(userID)
	select {
	case <-channel:
		t.Fatal("unsubscribed user received a signal")
	default:
	}
}
