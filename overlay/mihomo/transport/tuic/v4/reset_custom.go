// Problem:
// Upstream TUIC v4 clientImpl.Close() defers closing the QUIC connection until
// openStreams reaches zero, keeping the stale socket alive during interface
// failover.
//
// Solution:
// Expose ForceClose() to immediately mark the retired clientImpl closed and
// close its QUIC connection and UDP sessions.

package v4

import "github.com/metacubex/mihomo/transport/tuic/types"

// ForceClose immediately closes this retired TUIC v4 client instance regardless of openStreams.
func (t *clientImpl) ForceClose() {
	t.closed.Store(true)
	t.forceClose(nil, types.ClientClosed)
}
