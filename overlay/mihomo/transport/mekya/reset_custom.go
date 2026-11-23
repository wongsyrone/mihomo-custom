// Problem:
// mekya maintains pending pre-negotiated ALPN connections and HTTP/1.1 + HTTP/2
// transports bound to the previous default interface, while Client.Close()
// permanently cancels Client.ctx and sets pendingConn to nil.
//
// Solution:
// Close pending connections (replacing pendingConn with a fresh non-nil map)
// and close idle+H2 connections on h1/h2 transports while leaving Client.ctx active.

package mekya

import "github.com/metacubex/mihomo/common/httputils"

// ResetSessions closes pooled HTTP/1.1, HTTP/2, and pending connections without canceling the Client lifetime context.
func (c *Client) ResetSessions() {
	if c == nil || (c.ctx != nil && c.ctx.Err() != nil) {
		return
	}
	if rt, ok := c.rt.(*alpnAwareRoundTripper); ok {
		rt.resetSessions()
	}
}

func (r *alpnAwareRoundTripper) resetSessions() {
	r.mu.Lock()
	if r.pendingConn == nil {
		r.mu.Unlock()
		return
	}
	pending := r.pendingConn
	r.pendingConn = make(map[pendingConnKey]*pendingConn)
	r.mu.Unlock()

	for _, conn := range pending {
		conn.close()
	}
	httputils.CloseTransport(r.h1)
	httputils.CloseTransport(r.h2)
}
