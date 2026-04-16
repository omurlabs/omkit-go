package valkeystream_test

import (
	"context"
	"testing"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/valkeystream"
)

func skipIfNoValkey(t *testing.T) *valkeystream.Stream {
	t.Helper()
	addr := "localhost:6379"
	s, err := valkeystream.New(addr, "", "test-stream-"+t.Name(), "test-group", "test-consumer")
	if err != nil {
		t.Skipf("Valkey not available: %v", err)
	}
	t.Cleanup(func() {
		s.Client().Del(context.Background(), "test-stream-"+t.Name())
		s.Close()
	})
	return s
}

func TestStream_AddRead(t *testing.T) {
	s := skipIfNoValkey(t)
	ctx := context.Background()

	id, err := s.Add(ctx, map[string]string{"key": "value", "foo": "bar"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty message ID")
	}

	msgs, err := s.ReadGroup(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ReadGroup: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Values["key"] != "value" {
		t.Errorf("expected Values[key]=value, got %q", msgs[0].Values["key"])
	}
	if msgs[0].Values["foo"] != "bar" {
		t.Errorf("expected Values[foo]=bar, got %q", msgs[0].Values["foo"])
	}
}

func TestStream_AckDelete(t *testing.T) {
	s := skipIfNoValkey(t)
	ctx := context.Background()

	id, err := s.Add(ctx, map[string]string{"x": "1"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	msgs, err := s.ReadGroup(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ReadGroup: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected at least one message")
	}

	if err := s.Ack(ctx, id); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	n, err := s.Len(ctx)
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 0 {
		t.Errorf("expected stream length 0 after delete, got %d", n)
	}
}

func TestStream_Len(t *testing.T) {
	s := skipIfNoValkey(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := s.Add(ctx, map[string]string{"i": string(rune('0'+i))}); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	n, err := s.Len(ctx)
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 2 {
		t.Errorf("expected len=2, got %d", n)
	}
}
