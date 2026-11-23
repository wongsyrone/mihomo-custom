package session

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/mihomo/transport/anytls/padding"
)

func TestClientResetSessions(t *testing.T) {
	var factory atomic.Pointer[padding.PaddingFactory]
	padding.UpdatePaddingScheme(padding.DefaultPaddingScheme, &factory)

	var dialCount atomic.Int32
	var firstClientConn net.Conn
	client := NewClient(context.Background(), func(ctx context.Context) (net.Conn, error) {
		n := dialCount.Add(1)
		cConn, sConn := net.Pipe()
		go func() {
			defer sConn.Close()
			_, _ = io.Copy(io.Discard, sConn)
		}()
		if n == 1 {
			firstClientConn = cConn
		}
		return cConn, nil
	}, &factory, "", 30*time.Second, 30*time.Second, 0, false)
	defer client.Close()

	s1, err := client.CreateStream(context.Background())
	if err != nil {
		t.Fatalf("first CreateStream failed: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("first stream Close failed: %v", err)
	}

	client.idleSessionLock.Lock()
	idleBefore := client.idleSession.Len()
	client.idleSessionLock.Unlock()
	if idleBefore != 1 {
		t.Fatalf("expected 1 idle session before reset, got %d", idleBefore)
	}

	client.ResetSessions()

	client.idleSessionLock.Lock()
	idleAfter := client.idleSession.Len()
	client.idleSessionLock.Unlock()
	if idleAfter != 0 {
		t.Fatalf("expected 0 idle sessions after reset, got %d", idleAfter)
	}

	if _, err := firstClientConn.Write([]byte("x")); err == nil {
		t.Fatal("expected first underlying conn to be closed after ResetSessions")
	}

	s2, err := client.CreateStream(context.Background())
	if err != nil {
		t.Fatalf("second CreateStream after reset failed: %v", err)
	}
	_ = s2.Close()

	if got := dialCount.Load(); got != 2 {
		t.Fatalf("expected dialCount=2 after ResetSessions, got %d", got)
	}
}
