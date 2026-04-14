package valkeysub_test

import (
	"testing"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/valkeysub"
)

func TestNewSubscriber(t *testing.T) {
	s := valkeysub.New("localhost:6379", valkeysub.WithReconnectDelay(2*time.Second))
	if s == nil {
		t.Fatal("expected non-nil subscriber")
	}
	if s.ReconnectDelay() != 2*time.Second {
		t.Errorf("expected 2s, got %v", s.ReconnectDelay())
	}
}

func TestDefaultReconnectDelay(t *testing.T) {
	s := valkeysub.New("localhost:6379")
	if s.ReconnectDelay() != 5*time.Second {
		t.Errorf("expected 5s default, got %v", s.ReconnectDelay())
	}
}
