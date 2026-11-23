// Problem:
// Closing upper-level streams alone leaves AnyTLS pooled idle and active
// sessions alive or re-queued via stream close hooks onto the stale interface.
//
// Solution:
// Detach all tracked sessions and clear idleSession under locks first, then
// close each Session outside locks to avoid deadlocks and re-queuing races.

package session

import "github.com/metacubex/mihomo/transport/anytls/skiplist"

// ResetSessions closes all active and idle sessions without shutting down the client,
// forcing subsequent streams to dial fresh underlying connections (e.g. after a default interface switch).
func (c *Client) ResetSessions() {
	if c == nil {
		return
	}
	c.sessionsLock.Lock()
	sessionToClose := make([]*Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessionToClose = append(sessionToClose, session)
	}
	c.sessions = make(map[uint64]*Session)
	c.sessionsLock.Unlock()

	c.idleSessionLock.Lock()
	c.idleSession = skiplist.NewSkipList[uint64, *Session]()
	c.idleSessionLock.Unlock()

	for _, session := range sessionToClose {
		session.Close()
	}
}
