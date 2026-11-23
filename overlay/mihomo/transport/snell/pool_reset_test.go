package snell

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/transport/shadowsocks/shadowaead"
)

type resetTestConn struct {
	closed bool
}

func (c *resetTestConn) Read(b []byte) (int, error) {
	if len(b) > 0 {
		b[0] = CommandTunnel
		return 1, shadowaead.ErrZeroChunk
	}
	return 0, io.EOF
}
func (c *resetTestConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *resetTestConn) Close() error                     { c.closed = true; return nil }
func (c *resetTestConn) LocalAddr() net.Addr              { return nil }
func (c *resetTestConn) RemoteAddr() net.Addr             { return nil }
func (c *resetTestConn) SetDeadline(time.Time) error      { return nil }
func (c *resetTestConn) SetReadDeadline(time.Time) error  { return nil }
func (c *resetTestConn) SetWriteDeadline(time.Time) error { return nil }

func TestPoolResetEvictsIdleAndPreventsStaleRequeue(t *testing.T) {
	var created []*resetTestConn
	p := NewPool(func(context.Context) (*Snell, error) {
		raw := &resetTestConn{}
		created = append(created, raw)
		return &Snell{Conn: raw}, nil
	})

	c1, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	c2, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}

	// Return c1 to the pool as an idle reusable connection; keep c2 leased across Reset.
	pc1 := c1.(*PoolConn)
	pc1.MarkReusable()
	_, _ = pc1.Read(make([]byte, 1))
	if err := pc1.Close(); err != nil {
		t.Fatal(err)
	}
	if created[0].closed {
		t.Fatal("expected c1 to enter idle pool instead of closing")
	}

	p.Reset()
	if !created[0].closed {
		t.Fatal("expected idle c1 to be evicted and closed on Reset")
	}

	// Closing pre-reset lease c2 after Reset must close its raw connection, not re-pool it.
	pc2 := c2.(*PoolConn)
	pc2.MarkReusable()
	_, _ = pc2.Read(make([]byte, 1))
	if err := pc2.Close(); err != nil {
		t.Fatal(err)
	}
	if !created[1].closed {
		t.Fatal("expected pre-reset lease c2 to close instead of returning to pool")
	}

	c3, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != 3 || c3.(*PoolConn).Snell.Conn != created[2] {
		t.Fatalf("expected Get after Reset to dial fresh conn #3, got %d conns", len(created))
	}
}
