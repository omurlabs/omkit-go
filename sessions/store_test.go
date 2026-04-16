package sessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/sessions"
)

func TestStoreInterfaceShape(t *testing.T) {
	var _ sessions.Store = (*fakeStore)(nil)
}

type fakeStore struct{}

func (f *fakeStore) Get(ctx context.Context, token string) (*sessions.Session, error) {
	return nil, nil
}
func (f *fakeStore) Put(ctx context.Context, s *sessions.Session) error { return nil }
func (f *fakeStore) Delete(ctx context.Context, token string) error    { return nil }
func (f *fakeStore) List(ctx context.Context, tenantID string) ([]*sessions.Session, error) {
	return nil, nil
}
func (f *fakeStore) Close() error { return nil }

func TestSessionFields(t *testing.T) {
	s := &sessions.Session{
		Token:     "tok",
		TenantID:  "ten",
		Payload:   []byte(`{"k":"v"}`),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if s.Token != "tok" {
		t.Fatal("token field")
	}
}
