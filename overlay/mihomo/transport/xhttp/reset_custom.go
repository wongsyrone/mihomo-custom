// Problem:
// xhttp ReuseManager pools keep HTTP/2 / HTTP/3 transports open across
// interface changes, while Client.Close() cancels Client.ctx permanently.
//
// Solution:
// Reset uploadManager and downloadManager by detaching and closing cached
// reuseEntry transports without canceling Client.ctx.

package xhttp

// ResetSessions closes cached HTTP transports without preventing future GetTransport calls from creating new ones.
func (m *ReuseManager) ResetSessions() {
	if m == nil {
		return
	}
	m.mu.Lock()
	entries := m.entries
	m.entries = nil
	m.mu.Unlock()

	for _, entry := range entries {
		entry.close()
	}
}

// ResetSessions clears pooled upload and download transports while keeping the Client active for future dials.
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	if c.uploadManager != nil {
		c.uploadManager.ResetSessions()
	}
	if c.downloadManager != nil {
		c.downloadManager.ResetSessions()
	}
}
