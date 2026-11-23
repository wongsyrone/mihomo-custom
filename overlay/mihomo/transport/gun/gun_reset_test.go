package gun

import (
	"context"
	"net"
	"testing"
)

func TestClientResetSessions(t *testing.T) {
	client := NewClient(func() *Transport {
		return NewTransport(func(context.Context, string, string) (net.Conn, error) {
			return nil, context.Canceled
		}, nil, &Config{Host: "example.com"})
	}, 1, 1, 4)

	t1 := client.getTransport()
	client.ResetSessions()

	if t1.ctx.Err() == nil {
		t.Fatal("expected previous transport context to be canceled by ResetSessions")
	}

	t2 := client.getTransport()
	if t2 == t1 {
		t.Fatal("expected getTransport after ResetSessions to construct a new transport")
	}
	if t2.ctx.Err() != nil {
		t.Fatalf("expected new transport context to be active, got %v", t2.ctx.Err())
	}
	_ = client.Close()
}
