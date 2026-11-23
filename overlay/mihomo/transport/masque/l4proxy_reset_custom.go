// Problem:
// MASQUE L4Client holds a single cached *http3.ClientConn bound to the old
// network interface's UDP socket.
//
// Solution:
// Acquire runLock with a short timeout without canceling runCtx, detach
// clientConn, and close it so future DialContext calls establish a new HTTP/3
// connection.

package masque

import (
	"context"
	"time"
)

// ResetSessions clears the cached HTTP/3 ClientConn without canceling runCtx so future dials re-establish it.
func (c *L4Client) ResetSessions() {
	if c == nil || c.runLock == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := c.runLock.Acquire(ctx, 1)
	cancel()
	if err != nil {
		return
	}
	clientConn := c.clientConn
	c.clientConn = nil
	c.runLock.Release(1)

	if clientConn != nil {
		_ = clientConn.CloseWithError(0, "default interface changed")
	}
}
