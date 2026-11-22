package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Mtze/simple-cd/internal/store"
)

// Hub fans change events from store.Listen out to the open /api/events
// streams of this replica. The zero value is ready to use.
type Hub struct {
	mu   sync.Mutex
	subs map[chan store.Event]struct{}
}

// Publish never blocks: a stream that is behind drops the event and the UI
// catches up on its next poll.
func (h *Hub) Publish(e store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- e:
		default:
		}
	}
}

func (h *Hub) subscribe() (<-chan store.Event, func()) {
	c := make(chan store.Event, 16)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[chan store.Event]struct{}{}
	}
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c, func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}
}

// keepalive stays below the idle timeouts of proxies and gateways.
var keepalive = 25 * time.Second

// events streams change events as Server-Sent Events named "change".
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok || s.Events == nil {
		writeError(w, 500, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Events.subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(keepalive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": keepalive\n\n")
		case e := <-ch:
			b, _ := json.Marshal(e) // one line: JSON escapes line breaks
			fmt.Fprintf(w, "event: change\ndata: %s\n\n", b)
		}
		flusher.Flush()
	}
}
