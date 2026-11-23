// Problem:
// TUIC PoolClient reuses long-lived QUIC clients whose upstream Close() defers
// closing while openStreams > 0 or until idle expiration, keeping sockets stuck
// on a disconnected interface.
//
// Solution:
// Detach all pooled TCP and UDP clients, re-initialize the lists so future dials
// construct fresh clients on the new interface, and call ForceClose() on every
// detached client outside the mutexes.

package tuic

// ResetSessions forcefully closes all pooled QUIC clients and clears both TCP and UDP pools
// so subsequent dials construct fresh TUIC clients on the active network interface.
func (t *PoolClient) ResetSessions() {
	if t == nil {
		return
	}
	var toClose []Client

	t.tcpClientsMutex.Lock()
	for it := t.tcpClients.Front(); it != nil; it = it.Next() {
		if it.Value != nil {
			toClose = append(toClose, it.Value)
		}
	}
	t.tcpClients.Init()
	t.tcpClientsMutex.Unlock()

	t.udpClientsMutex.Lock()
	for it := t.udpClients.Front(); it != nil; it = it.Next() {
		if it.Value != nil {
			toClose = append(toClose, it.Value)
		}
	}
	t.udpClients.Init()
	t.udpClientsMutex.Unlock()

	for _, client := range toClose {
		client.ForceClose()
	}
}
