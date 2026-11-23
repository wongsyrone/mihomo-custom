// Problem:
// Pooled TCP protocols (such as Snell) need to flush idle sockets when the
// default outbound interface switches without destroying the pool itself.
//
// Solution:
// Drain buffered entries in a non-blocking loop and run the pool's evict
// callback on each removed item.

package pool

// Reset drains all currently buffered entries from the pool and invokes evict on each.
func (p *pool[T]) Reset() {
	if p == nil {
		return
	}
	for {
		select {
		case item, ok := <-p.ch:
			if !ok {
				return
			}
			if p.evict != nil {
				p.evict(item.elm)
			}
		default:
			return
		}
	}
}
