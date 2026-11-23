// Problem:
// Cached AnyTLS sessions remain bound to the previous default interface until
// manually reset.
//
// Solution:
// Forward ResetSessions() to the underlying session.Client when non-nil.

package anytls

// ResetSessions clears pooled sessions bound to a stale network interface.
func (c *Client) ResetSessions() {
	if c != nil && c.sessionClient != nil {
		c.sessionClient.ResetSessions()
	}
}
