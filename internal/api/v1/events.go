package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/savorum/savorum/internal/event"
	"github.com/savorum/savorum/internal/identity"
)

type eventInput struct {
	LastID string `header:"Last-Event-Id" maxLength:"100"`
	After  string `query:"after" maxLength:"100"`
}

type eventConnections struct {
	sync.Mutex
	users map[string]int
	total int
}

func registerEvents(api huma.API, reader event.Reader) {
	connections := &eventConnections{users: map[string]int{}}

	huma.Register(api, huma.Operation{OperationID: "get-library-events", Method: http.MethodGet, Path: "/api/v1/events", Summary: "Follow library changes", Description: "Authorized server-sent events use the versioned domain envelope. Send Last-Event-ID (or after) to resume up to seven days of retained history. A resync event requires a normal API refresh; cursor events advance past irrelevant facts. Authorization is rechecked every second. There are at most four streams per account, 1024 per instance, 100 events per page, 15-second heartbeats, and five-minute connections. Reconnect with backoff; correctness never depends on this connection. Tokens are accepted only in Authorization, never query parameters.", Tags: []string{"Events"}, Responses: map[string]*huma.Response{"200": {Description: "Bounded, resumable event stream", Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: "string"}}}}}}, func(ctx context.Context, in *eventInput) (*huma.StreamResponse, error) {
		after := in.LastID
		if after == "" {
			after = in.After
		}

		page, err := reader.ReadEvents(ctx, after, 100)
		if err != nil {
			return nil, mapError(err)
		}

		p, _ := identity.FromContext(ctx)

		if !connections.acquire(p.UserID) {
			return nil, problem(429, "event-stream-limit", "Close another live connection and retry")
		}

		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer connections.release(p.UserID)

			_, w := humago.Unwrap(hctx)
			streamEvents(ctx, w, reader, page, after)
		}}, nil
	})
}

func (c *eventConnections) acquire(userID string) bool {
	c.Lock()
	defer c.Unlock()

	if c.users[userID] >= 4 || c.total >= 1024 {
		return false
	}

	c.users[userID]++
	c.total++

	return true
}

func (c *eventConnections) release(userID string) {
	c.Lock()
	defer c.Unlock()

	c.total--

	c.users[userID]--
	if c.users[userID] == 0 {
		delete(c.users, userID)
	}
}

type eventWriter struct {
	writer  http.ResponseWriter
	control *http.ResponseController
}

func (s eventWriter) send(name, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode event frame: %w", err)
	}

	if err := s.control.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("bound event write: %w", err)
	}

	if _, err := fmt.Fprintf(s.writer, "id: %s\nevent: %s\ndata: %s\n\n", id, name, data); err != nil {
		return fmt.Errorf("write event frame: %w", err)
	}

	if err := s.control.Flush(); err != nil {
		return fmt.Errorf("flush event frame: %w", err)
	}

	return nil
}

func (s eventWriter) page(page event.Page) error {
	if page.Resync {
		if err := s.send("resync", page.Cursor, map[string]string{"reason": "refresh-required"}); err != nil {
			return err
		}
	}

	for _, fact := range page.Events {
		if err := s.send("change", fact.ID, fact); err != nil {
			return err
		}
	}

	return nil
}

func streamEvents(ctx context.Context, w http.ResponseWriter, reader event.Reader, page event.Page, after string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	writer := eventWriter{writer: w, control: http.NewResponseController(w)}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	lifetime := time.NewTimer(5 * time.Minute)
	defer lifetime.Stop()

	heartbeat := time.Now()

	for {
		if err := writer.page(page); err != nil {
			return
		}

		if page.Cursor != after || time.Since(heartbeat) >= 15*time.Second {
			if err := writer.send("cursor", page.Cursor, struct{}{}); err != nil {
				return
			}

			heartbeat = time.Now()
		}

		after = page.Cursor

		select {
		case <-ctx.Done():
			return
		case <-lifetime.C:
			return
		case <-ticker.C:
		}

		var err error

		page, err = reader.ReadEvents(ctx, after, 100)
		if err != nil {
			return
		}
	}
}
