// Problem:
// Snell reuses idle TCP connections through component/pool; flushing only the
// idle queue is insufficient if an in-flight pre-reset PoolConn finishes later
// and tries to re-enter the pool.
//
// Solution:
// Advance Pool.gen atomically and drain buffered connections so stale leases
// observe generation mismatch on Close() and close directly instead of pooling.

package snell

// Reset evicts all idle pooled connections and advances the generation so pre-reset leases close instead of re-pooling.
func (p *Pool) Reset() {
	if p == nil || p.pool == nil {
		return
	}
	p.gen.Add(1)
	p.pool.Reset()
}
