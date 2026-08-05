package agentview

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// spinner draws an in-place status line (CR + clear) while work is in flight.
// Only used when writing to a TTY.
type spinner struct {
	w      io.Writer
	style  func(string) string // optional color wrap for frame
	mu     sync.Mutex
	stopCh chan struct{}
	doneCh chan struct{}
	label  string
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (v *View) startSpinnerLocked(label string) {
	v.stopSpinnerLocked()
	if !isTerminalWriter(v.w) {
		return
	}
	s := &spinner{
		w:      v.w,
		style:  func(x string) string { return v.sIconRun.Render(x) },
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
		label:  label,
	}
	v.spin = s
	go s.run()
}

func (v *View) stopSpinnerLocked() {
	if v.spin == nil {
		return
	}
	v.spin.stop()
	v.spin = nil
}

func (s *spinner) run() {
	defer close(s.doneCh)
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	i := 0
	// Draw immediately so the user sees feedback before the first tick.
	s.draw(i)
	for {
		select {
		case <-s.stopCh:
			s.clear()
			return
		case <-t.C:
			i++
			s.draw(i)
		}
	}
}

func (s *spinner) draw(i int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame := spinnerFrames[i%len(spinnerFrames)]
	if s.style != nil {
		frame = s.style(frame)
	}
	// \r return + \033[K clear to end of line
	fmt.Fprintf(s.w, "\r\033[K  %s %s", frame, s.label)
}

func (s *spinner) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.w, "\r\033[K")
}

func (s *spinner) stop() {
	select {
	case <-s.doneCh:
		return // already stopped
	default:
	}
	close(s.stopCh)
	<-s.doneCh
}
