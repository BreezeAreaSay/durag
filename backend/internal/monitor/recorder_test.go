package monitor

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRecorderKeepsCountersEventsAndLogs(t *testing.T) {
	r := New()
	r.Inc("moves")
	r.Add("moves", 2)
	r.Event("error", "CANNOT_BEAT", "player", "dev:A", "room", "X")
	var out bytes.Buffer
	logger := slog.New(r.Handler(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelWarn}), slog.LevelInfo))
	logger.Info("bots: model move", "bot", "bot:1", "model", "deepseek/x:free")
	logger.With("room", "X").Warn("ws: intent failed", "err", "boom")
	logger.Debug("noise")
	snap := r.Snapshot(10, 10)
	if snap.Counters["moves"] != 3 {
		t.Fatalf("counter: %v", snap.Counters)
	}
	if len(snap.Events) != 1 || snap.Events[0].Fields["player"] != "dev:A" {
		t.Fatalf("events: %+v", snap.Events)
	}
	if len(snap.Logs) != 2 || snap.Logs[0].Level != "INFO" || !strings.Contains(snap.Logs[1].Attrs, "room=X") || !strings.Contains(snap.Logs[1].Attrs, "err=boom") {
		t.Fatalf("logs: %+v", snap.Logs)
	}
	// the wrapped handler still gets what its own level allows
	if strings.Contains(out.String(), "model move") || !strings.Contains(out.String(), "intent failed") {
		t.Fatalf("next handler output: %s", out.String())
	}
	var nilRec *Recorder
	nilRec.Inc("x") // a nil recorder is a no-op
	nilRec.Event("k", "m")
	if s := nilRec.Snapshot(1, 1); len(s.Counters) != 0 {
		t.Fatal("nil snapshot")
	}
}

func TestRecorderRingsAreBounded(t *testing.T) {
	r := New()
	r.maxEvents, r.maxLogs = 3, 2
	for i := 0; i < 10; i++ {
		r.Event("k", "m")
		r.logLine(r.started, "INFO", "l", "")
	}
	snap := r.Snapshot(0, 0)
	if len(snap.Events) != 3 || len(snap.Logs) != 2 {
		t.Fatalf("rings: %d events %d logs", len(snap.Events), len(snap.Logs))
	}
}
