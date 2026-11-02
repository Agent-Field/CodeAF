package taskdoor

import "time"

// Keepalive has its own writer so a peer that stops reading cannot block the
// clock loop that must eventually close that very peer.
func (c *Conn) startHeartbeat() {
	c.heartbeatOnce.Do(func() {
		c.ping = make(chan struct{}, 1)
		c.pong = make(chan struct{}, 1)
		ticker := c.clock.NewTicker(PingEvery)
		silence := c.clock.NewTicker(SilentAfter)
		go func() {
			defer ticker.Stop()
			defer func() { silence.Stop() }()
			resetSilence := func() bool {
				remaining := SilentAfter - time.Duration(c.clock.Now().UnixNano()-c.lastFrame.Load())
				if remaining <= 0 {
					c.closeTransport()
					return false
				}
				silence.Stop()
				silence = c.clock.NewTicker(remaining)
				return true
			}
			for {
				select {
				case <-c.done:
					return
				case <-c.activity:
					if c.watching.Load() && !resetSilence() {
						return
					}
				case <-silence.C():
					if c.watching.Load() && !resetSilence() {
						return
					}
				case <-ticker.C():
					if !c.watching.Load() {
						continue
					}
					if c.clock.Now().UnixNano()-c.lastFrame.Load() >= int64(SilentAfter) {
						c.closeTransport()
						return
					}
					select {
					case c.ping <- struct{}{}:
					default:
					}
				}
			}
		}()
		go func() {
			for {
				var payload any
				select {
				case <-c.done:
					return
				case <-c.ping:
					payload = Ping{}
				case <-c.pong:
					payload = Pong{}
				}
				frame, err := NewFrame("", payload)
				if err != nil || c.sendFrame(frame) != nil {
					return
				}
			}
		}()
	})
}
func (c *Conn) queuePong() {
	select {
	case c.pong <- struct{}{}:
	default:
	}
}
func (c *Conn) startWatch() {
	if !c.watching.Swap(true) {
		c.lastFrame.Store(c.clock.Now().UnixNano())
	}
	c.startHeartbeat()
	c.noteActivity()
}

// A separate silence wakeup preserves the full interval after every frame;
// checking only on probe ticks could otherwise close a connection late.
func (c *Conn) noteActivity() {
	select {
	case c.activity <- struct{}{}:
	default:
	}
}
