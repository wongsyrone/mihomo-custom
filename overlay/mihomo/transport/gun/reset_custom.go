// Problem:
// gRPC (gun) pools long-lived HTTP/2 Transports inside Client.transports that
// remain bound to the previous default interface until closed.
//
// Solution:
// Detach and close cached transports outside mutex so subsequent getTransport()
// calls dial fresh HTTP/2 connections without permanently disabling Client.

package gun

// ResetSessions closes existing HTTP/2 transports without disabling the Client so subsequent dials open fresh connections.
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	c.mutex.Lock()
	transports := c.transports
	c.transports = nil
	c.mutex.Unlock()

	for _, t := range transports {
		_ = t.Close()
	}
}
