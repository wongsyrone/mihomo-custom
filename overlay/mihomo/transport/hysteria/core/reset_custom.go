// Problem:
// Hysteria (v1) caches a single *quic.Conn in c.quicSession until an error or
// permanent Close(), stalling over a switched default interface.
//
// Solution:
// Detach and close quicSession under reconnectMutex without setting c.closed
// so the next openStreamWithReconnect dials over the active interface.

package core

// ResetSessions closes the active QUIC connection so the next openStreamWithReconnect reconnects over the new interface.
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	c.reconnectMutex.Lock()
	qs := c.quicSession
	c.quicSession = nil
	c.reconnectMutex.Unlock()

	if qs != nil {
		_ = qs.CloseWithError(closeErrorCodeGeneric, "default interface changed")
	}
}
