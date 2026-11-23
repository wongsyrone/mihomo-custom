package mekya

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestClientResetSessions(t *testing.T) {
	client, err := NewClient(context.Background(), func(context.Context) (net.Conn, error) {
		return nil, errors.New("not dialed")
	}, Config{URL: "https://example.com/tun"})
	if err != nil {
		t.Fatal(err)
	}

	rt := client.rt.(*alpnAwareRoundTripper)
	cConn, sConn := net.Pipe()
	defer sConn.Close()

	rt.mu.Lock()
	rt.putPendingConnLocked("example.com:443", false, cConn)
	rt.mu.Unlock()

	client.ResetSessions()

	if _, err := cConn.Write([]byte("x")); err == nil {
		t.Fatal("expected pending connection to be closed on ResetSessions")
	}
	rt.mu.Lock()
	if rt.pendingConn == nil || len(rt.pendingConn) != 0 {
		rt.mu.Unlock()
		t.Fatal("expected non-nil empty pendingConn map after ResetSessions")
	}
	rt.mu.Unlock()

	if client.ctx.Err() != nil {
		t.Fatalf("expected mekya Client context to stay active after ResetSessions, got %v", client.ctx.Err())
	}
	_ = client.Close()
}
