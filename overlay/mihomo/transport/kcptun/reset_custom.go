// Problem:
// kcptun caches smux/KCP sessions in Client.muxes, while Client.Close()
// permanently cancels c.ctx (and stops the scavenger goroutine).
//
// Solution:
// Detach and close non-nil smux sessions under connMu while keeping Client.ctx
// alive for subsequent OpenStream dials.

package kcptun

import "github.com/metacubex/smux"

// ResetSessions detaches and closes active smux/kcp sessions without canceling the client context.
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	c.connMu.Lock()
	var toClose []*smux.Session
	for i := range c.muxes {
		if c.muxes[i].session != nil {
			toClose = append(toClose, c.muxes[i].session)
			c.muxes[i].session = nil
		}
	}
	c.connMu.Unlock()

	for _, s := range toClose {
		_ = s.Close()
	}
}
