// Package monitor keeps what an admin wants to see without a log stack:
// counters, the recent notable events and the recent log lines, in memory.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Event is something worth a line in the admin panel.
type Event struct {
	At      time.Time         `json:"at"`
	Kind    string            `json:"kind"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// LogLine is one record the slog handler saw.
type LogLine struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Attrs   string    `json:"attrs,omitempty"`
}

// Snapshot is what the admin endpoint returns from the recorder.
type Snapshot struct {
	Started       time.Time        `json:"started"`
	UptimeSeconds int64            `json:"uptime_seconds"`
	Counters      map[string]int64 `json:"counters"`
	Events        []Event          `json:"events"`
	Logs          []LogLine        `json:"logs"`
}

// Recorder is safe for concurrent use.
type Recorder struct {
	mu        sync.Mutex
	started   time.Time
	counters  map[string]int64
	events    []Event
	logs      []LogLine
	maxEvents int
	maxLogs   int
}

// New creates a recorder keeping the last few hundred events and log lines.
func New() *Recorder {
	return &Recorder{started: time.Now(), counters: map[string]int64{}, maxEvents: 400, maxLogs: 400}
}

// Inc bumps a counter.
func (r *Recorder) Inc(name string) { r.Add(name, 1) }

// Add adds n to a counter.
func (r *Recorder) Add(name string, n int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.counters[name] += n
	r.mu.Unlock()
}

// Event records a notable event; kv are alternating keys and values.
func (r *Recorder) Event(kind, message string, kv ...any) {
	if r == nil {
		return
	}
	fields := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		fields[fmt.Sprint(kv[i])] = fmt.Sprint(kv[i+1])
	}
	r.mu.Lock()
	r.events = append(r.events, Event{At: time.Now(), Kind: kind, Message: message, Fields: fields})
	if len(r.events) > r.maxEvents {
		r.events = r.events[len(r.events)-r.maxEvents:]
	}
	r.mu.Unlock()
}

func (r *Recorder) logLine(at time.Time, level, message, attrs string) {
	r.mu.Lock()
	r.logs = append(r.logs, LogLine{At: at, Level: level, Message: message, Attrs: attrs})
	if len(r.logs) > r.maxLogs {
		r.logs = r.logs[len(r.logs)-r.maxLogs:]
	}
	r.mu.Unlock()
}

// Snapshot returns the counters and the last events and log lines (newest last).
func (r *Recorder) Snapshot(maxEvents, maxLogs int) Snapshot {
	if r == nil {
		return Snapshot{Counters: map[string]int64{}}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	counters := make(map[string]int64, len(r.counters))
	for k, v := range r.counters {
		counters[k] = v
	}
	events := r.events
	if maxEvents > 0 && len(events) > maxEvents {
		events = events[len(events)-maxEvents:]
	}
	logs := r.logs
	if maxLogs > 0 && len(logs) > maxLogs {
		logs = logs[len(logs)-maxLogs:]
	}
	return Snapshot{
		Started:       r.started,
		UptimeSeconds: int64(time.Since(r.started).Seconds()),
		Counters:      counters,
		Events:        append([]Event{}, events...),
		Logs:          append([]LogLine{}, logs...),
	}
}

// CounterNames returns the counter names, sorted (tests, display).
func (r *Recorder) CounterNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.counters))
	for k := range r.counters {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Handler wraps a slog handler: records at or above min are also kept in the
// recorder, everything still reaches next.
func (r *Recorder) Handler(next slog.Handler, min slog.Level) slog.Handler {
	return &teeHandler{r: r, next: next, min: min}
}

type teeHandler struct {
	r     *Recorder
	next  slog.Handler
	min   slog.Level
	attrs []slog.Attr
}

func (h *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.min || h.next.Enabled(ctx, level)
}

func (h *teeHandler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= h.min {
		var sb strings.Builder
		for _, a := range h.attrs {
			fmt.Fprintf(&sb, "%s=%v ", a.Key, a.Value)
		}
		rec.Attrs(func(a slog.Attr) bool {
			fmt.Fprintf(&sb, "%s=%v ", a.Key, a.Value)
			return true
		})
		h.r.logLine(rec.Time, rec.Level.String(), rec.Message, strings.TrimSpace(sb.String()))
	}
	if h.next.Enabled(ctx, rec.Level) {
		return h.next.Handle(ctx, rec)
	}
	return nil
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{r: h.r, next: h.next.WithAttrs(attrs), min: h.min, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{r: h.r, next: h.next.WithGroup(name), min: h.min, attrs: h.attrs}
}
