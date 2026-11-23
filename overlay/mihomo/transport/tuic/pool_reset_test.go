package tuic

import (
	"context"
	"net"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/tuic/types"

	"github.com/metacubex/quic-go"
)

type fakeTuicClient struct {
	openStreams int64
	lastVisited time.Time
	forceClosed bool
}

func (f *fakeTuicClient) DialContext(context.Context, *C.Metadata) (net.Conn, error) {
	return nil, nil
}
func (f *fakeTuicClient) ListenPacket(context.Context, *C.Metadata) (net.PacketConn, error) {
	return nil, nil
}
func (f *fakeTuicClient) OpenStreams() int64         { return f.openStreams }
func (f *fakeTuicClient) LastVisited() time.Time     { return f.lastVisited }
func (f *fakeTuicClient) SetLastVisited(t time.Time) { f.lastVisited = t }
func (f *fakeTuicClient) Close()                     {}
func (f *fakeTuicClient) ForceClose()                { f.forceClosed = true }

func TestPoolClientResetSessions(t *testing.T) {
	pool := NewPoolClientV5(&ClientOptionV5{MaxOpenStreams: 100}, func(context.Context) (*quic.Conn, error) {
		return nil, context.Canceled
	})

	tcpFake := &fakeTuicClient{openStreams: 3}
	udpFake := &fakeTuicClient{openStreams: 2}

	pool.tcpClientsMutex.Lock()
	pool.tcpClients.PushFront(tcpFake)
	pool.tcpClientsMutex.Unlock()

	pool.udpClientsMutex.Lock()
	pool.udpClients.PushFront(udpFake)
	pool.udpClientsMutex.Unlock()

	pool.ResetSessions()

	if !tcpFake.forceClosed || !udpFake.forceClosed {
		t.Fatalf("expected ForceClose on pooled clients, got tcp=%v udp=%v", tcpFake.forceClosed, udpFake.forceClosed)
	}
	if pool.tcpClients.Len() != 0 || pool.udpClients.Len() != 0 {
		t.Fatalf("expected cleared client lists, got tcp=%d udp=%d", pool.tcpClients.Len(), pool.udpClients.Len())
	}

	nextTCP := pool.getClient(false)
	nextUDP := pool.getClient(true)
	if nextTCP == types.Client(tcpFake) || nextUDP == types.Client(udpFake) {
		t.Fatal("expected getClient after ResetSessions to allocate fresh TUIC clients")
	}
}
