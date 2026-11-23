// Problem:
// TrustTunnel PoolClient holds pooled HTTP/2 or HTTP/3 Clients bound to the
// previous interface.
//
// Solution:
// Detach and close pooled Clients under mutex so getClient() constructs fresh
// transports on subsequent dials.

package trusttunnel

// ResetSessions closes pooled HTTP/2 or HTTP/3 transports while keeping PoolClient ready to dial fresh ones.
func (c *PoolClient) ResetSessions() {
	if c == nil {
		return
	}
	c.mutex.Lock()
	clients := c.clients
	c.clients = nil
	c.mutex.Unlock()

	for _, client := range clients {
		_ = client.Close()
	}
}
