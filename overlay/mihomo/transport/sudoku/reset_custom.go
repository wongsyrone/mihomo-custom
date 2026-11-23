// Problem:
// Sudoku MultiplexDialer maintains an active mux client (plus maintain loop)
// while Close() sets d.closed = true permanently and tears down the maintainer.
//
// Solution:
// Abort any in-flight creation and close the current client without setting
// d.closed so the next Dial re-establishes a mux session on the active interface.

package sudoku

// ResetSessions closes the active multiplex client and aborts in-flight creation without closing the dialer.
func (d *MultiplexDialer) ResetSessions() {
	if d == nil {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	stop := d.createStop
	client := d.client
	d.client = nil
	d.mu.Unlock()

	if stop != nil {
		stop()
	}
	if client != nil {
		_ = client.Close()
	}
}
