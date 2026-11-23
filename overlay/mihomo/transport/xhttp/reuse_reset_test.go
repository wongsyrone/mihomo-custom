package xhttp

import (
	"sync/atomic"
	"testing"

	"github.com/metacubex/http"
)

func TestReuseManagerAndClientResetSessions(t *testing.T) {
	var created atomic.Int64
	client, err := NewClient(
		&Config{
			Host: "example.com",
			Mode: "stream-one",
			ReuseConfig: &ReuseConfig{
				MaxConcurrency:   "4",
				MaxConnections:   "1",
				HMaxRequestTimes: "100",
			},
		},
		func() http.RoundTripper {
			created.Add(1)
			return &http.Transport{}
		},
		nil,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	rt1 := client.uploadManager.GetTransport().(*ReuseTransport)
	entry1 := rt1.entry
	_ = rt1.Close()

	client.ResetSessions()
	if !entry1.isClosed() {
		t.Fatal("expected cached upload reuseEntry to be closed on ResetSessions")
	}
	if client.ctx.Err() != nil {
		t.Fatalf("expected Client context to stay active across ResetSessions, got %v", client.ctx.Err())
	}

	rt2 := client.uploadManager.GetTransport().(*ReuseTransport)
	if rt2.entry == entry1 {
		t.Fatal("expected new upload reuseEntry after ResetSessions")
	}
	if created.Load() != 2 {
		t.Fatalf("expected 2 transports created, got %d", created.Load())
	}
	_ = rt2.Close()
	_ = client.Close()
}
