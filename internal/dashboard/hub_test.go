package dashboard

import "testing"

func TestHubDeliversInvalidateToSubscriber(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe("ws-1")
	defer unsubscribe()

	hub.Invalidate("ws-1")

	select {
	case <-ch:
	default:
		t.Fatal("expected a pending signal after Invalidate")
	}
}

func TestHubInvalidateIsNonBlockingWhenAlreadyDirty(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe("ws-1")
	defer unsubscribe()

	// Two invalidations with nothing draining the channel between them must not block;
	// the second is coalesced into the first pending signal.
	hub.Invalidate("ws-1")
	hub.Invalidate("ws-1")

	select {
	case <-ch:
	default:
		t.Fatal("expected a pending signal")
	}
	select {
	case <-ch:
		t.Fatal("expected exactly one coalesced signal, got a second")
	default:
	}
}

func TestHubInvalidateOnUnknownWorkspaceIsNoop(t *testing.T) {
	hub := NewHub()
	hub.Invalidate("nobody-subscribed") // must not panic
}

func TestHubDoesNotCrossDeliverBetweenWorkspaces(t *testing.T) {
	hub := NewHub()
	chA, unsubA := hub.Subscribe("ws-a")
	defer unsubA()
	chB, unsubB := hub.Subscribe("ws-b")
	defer unsubB()

	hub.Invalidate("ws-a")

	select {
	case <-chA:
	default:
		t.Fatal("ws-a subscriber should have been signaled")
	}
	select {
	case <-chB:
		t.Fatal("ws-b subscriber should not have been signaled")
	default:
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe("ws-1")
	unsubscribe()

	_, open := <-ch
	if open {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}
