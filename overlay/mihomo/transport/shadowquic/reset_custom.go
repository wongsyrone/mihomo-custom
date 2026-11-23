// Problem:
// ShadowQuic keeps a single *connState in Client.conn, and Client.Close() sets
// c.closed = true permanently.
//
// Solution:
// Detach and close Client.conn under lock without setting c.closed so subsequent
// dials recreate connState over the active interface.

package shadowquic

// ResetSessions closes the active QUIC state without setting c.closed so subsequent dials recreate it.
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	state := c.conn
	c.conn = nil
	c.mu.Unlock()

	if state != nil {
		_ = state.closeWithError(0, "default interface changed")
	}
}
