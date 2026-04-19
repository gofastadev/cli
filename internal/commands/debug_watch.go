package commands

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofastadev/cli/internal/clierr"
	"github.com/gofastadev/cli/internal/termcolor"
	"github.com/spf13/cobra"
)

var (
	debugWatchInterval string
	debugWatchTrace    bool
	debugWatchSQL      bool
	debugWatchErrors   bool
	debugWatchCache    bool
	debugWatchRequests bool
)

// debugWatchCmd streams NDJSON events as new entries appear in each
// /debug/* ring. One event per line so jq / shell pipelines consume
// it naturally. Default channels: requests + errors (the two an
// agent almost always wants). The other channels are opt-in via
// their --with-* flag.
//
// Design note: this command is inherently polling. The scaffold's
// /debug/* endpoints don't push; they return snapshots. We de-dup
// against each ring's first entry's (time, id) per tick so each
// event is emitted exactly once.
var debugWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Stream NDJSON events as new debug entries land",
	Long: `Polls every /debug/* surface and emits one JSON line per new
entry discovered. Ctrl+C exits cleanly. Each event has an ` + "`event`" + `
field identifying its source (request, sql, error, cache) so
downstream filters can branch.

A heartbeat event fires every 30 seconds while no new entries
appear so pipelines confirm the command is still live.

Default channels: requests + errors. Enable more with --sql,
--cache, or --trace. --interval controls the poll cadence.

Examples:

  gofasta debug watch
  gofasta debug watch --sql --cache
  gofasta debug watch --errors | jq -c 'select(.recovered != null)'
  gofasta debug watch --interval=500ms`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return runDebugWatch()
	},
}

func init() {
	debugWatchCmd.Flags().StringVar(&debugWatchInterval, "interval", "1s",
		"Poll interval (Go duration syntax — 500ms, 2s, …)")
	debugWatchCmd.Flags().BoolVar(&debugWatchRequests, "requests", true,
		"Emit events for newly-captured requests")
	debugWatchCmd.Flags().BoolVar(&debugWatchErrors, "errors", true,
		"Emit events for newly-recovered panics")
	debugWatchCmd.Flags().BoolVar(&debugWatchSQL, "sql", false,
		"Emit events for newly-captured SQL statements")
	debugWatchCmd.Flags().BoolVar(&debugWatchCache, "cache", false,
		"Emit events for newly-captured cache ops")
	debugWatchCmd.Flags().BoolVar(&debugWatchTrace, "trace", false,
		"Emit events for newly-completed traces")
	debugCmd.AddCommand(debugWatchCmd)
}

// runDebugWatch is the main loop. Each ring gets its own high-water
// mark (latest emitted time) so each tick only emits events strictly
// newer than the last known.
func runDebugWatch() error {
	appURL := resolveAppURL()
	if err := requireDevtools(appURL); err != nil {
		return err
	}
	interval, err := time.ParseDuration(debugWatchInterval)
	if err != nil {
		return clierr.Wrapf(clierr.CodeDebugBadDuration, err,
			"invalid --interval value %q", debugWatchInterval)
	}

	// Signal handling — Ctrl+C sets ctx.Done() so the poll loop exits
	// without printing a Go panic / stack.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// High-water marks are per-channel to keep each stream
	// independent. Using time.Time instead of opaque cursors lets us
	// tolerate ring evictions cleanly: if the ring drops entries we
	// haven't seen, we still emit only new ones.
	marks := watchMarks{}
	// First tick baselines the marks against the current rings so we
	// don't dump the entire history on startup. After baseline, every
	// subsequent tick emits only newer entries.
	marks.baseline(appURL)

	emitter := newWatchEmitter(os.Stdout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	fprintln(os.Stderr, termcolor.CDim("watching "+appURL+" — Ctrl+C to stop"))
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			emitter.heartbeat()
		case <-ticker.C:
			marks.pollAndEmit(appURL, emitter)
		}
	}
}

// watchMarks holds the last-emitted timestamp for each channel. Any
// ring entry with a Time strictly newer is a new event.
type watchMarks struct {
	mu         sync.Mutex
	request    time.Time
	sql        time.Time
	errorsMark time.Time
	cacheMark  time.Time
	trace      time.Time
}

// baseline initializes each mark to the newest entry currently in the
// corresponding ring so startup doesn't flood stdout with history.
func (m *watchMarks) baseline(appURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if debugWatchRequests {
		var rs []scrapedRequest
		_ = getJSON(appURL, "/debug/requests", &rs)
		if len(rs) > 0 {
			m.request = rs[0].Time
		}
	}
	if debugWatchErrors {
		var es []scrapedException
		_ = getJSON(appURL, "/debug/errors", &es)
		if len(es) > 0 {
			m.errorsMark = es[0].Time
		}
	}
	if debugWatchSQL {
		var qs []scrapedQuery
		_ = getJSON(appURL, "/debug/sql", &qs)
		if len(qs) > 0 {
			m.sql = qs[0].Time
		}
	}
	if debugWatchCache {
		var cs []scrapedCache
		_ = getJSON(appURL, "/debug/cache", &cs)
		if len(cs) > 0 {
			m.cacheMark = cs[0].Time
		}
	}
	if debugWatchTrace {
		var ts []scrapedTrace
		_ = getJSON(appURL, "/debug/traces", &ts)
		if len(ts) > 0 {
			m.trace = ts[0].Time
		}
	}
}

// pollAndEmit queries every enabled channel and emits new entries.
// Each channel lives in its own helper so this dispatcher stays
// flat.
func (m *watchMarks) pollAndEmit(appURL string, e *watchEmitter) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if debugWatchRequests {
		m.request = pollChannel(appURL, "/debug/requests", m.request, func(r scrapedRequest) time.Time { return r.Time }, e.emitRequest)
	}
	if debugWatchErrors {
		m.errorsMark = pollChannel(appURL, "/debug/errors", m.errorsMark, func(x scrapedException) time.Time { return x.Time }, e.emitError)
	}
	if debugWatchSQL {
		m.sql = pollChannel(appURL, "/debug/sql", m.sql, func(q scrapedQuery) time.Time { return q.Time }, e.emitSQL)
	}
	if debugWatchCache {
		m.cacheMark = pollChannel(appURL, "/debug/cache", m.cacheMark, func(c scrapedCache) time.Time { return c.Time }, e.emitCache)
	}
	if debugWatchTrace {
		m.trace = pollChannel(appURL, "/debug/traces", m.trace, func(t scrapedTrace) time.Time { return t.Time }, e.emitTrace)
	}
}

// pollChannel is the generic "fetch, filter-by-high-water-mark, emit"
// loop parameterized over the entry type. timeOf extracts a Time
// from each entry; emit is the channel-specific writer.
//
// Rings come back newest-first; we walk them backwards (oldest to
// newest) so emitted events preserve causal order in the output
// stream. The returned value is the new high-water mark.
func pollChannel[T any](appURL, path string, mark time.Time, timeOf func(T) time.Time, emit func(T)) time.Time {
	var entries []T
	if err := getJSON(appURL, path, &entries); err != nil {
		return mark
	}
	newest := mark
	for i := len(entries) - 1; i >= 0; i-- {
		t := timeOf(entries[i])
		if !t.After(mark) {
			continue
		}
		emit(entries[i])
		if t.After(newest) {
			newest = t
		}
	}
	return newest
}

// watchEmitter writes one NDJSON line per event. Its public methods
// are intentionally narrow — each event shape is fixed. Serialization
// goes through encoding/json directly; we don't need cliout here
// because watch is always JSON (text mode would defeat the purpose).
type watchEmitter struct {
	enc *json.Encoder
}

func newWatchEmitter(w io.Writer) *watchEmitter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &watchEmitter{enc: enc}
}

func (e *watchEmitter) emitRequest(r scrapedRequest) {
	_ = e.enc.Encode(wrapWatchEvent("request", r))
}
func (e *watchEmitter) emitError(ex scrapedException) {
	_ = e.enc.Encode(wrapWatchEvent("error", ex))
}
func (e *watchEmitter) emitSQL(q scrapedQuery) {
	_ = e.enc.Encode(wrapWatchEvent("sql", q))
}
func (e *watchEmitter) emitCache(c scrapedCache) {
	_ = e.enc.Encode(wrapWatchEvent("cache", c))
}
func (e *watchEmitter) emitTrace(tr scrapedTrace) {
	_ = e.enc.Encode(wrapWatchEvent("trace", tr))
}
func (e *watchEmitter) heartbeat() {
	_ = e.enc.Encode(map[string]interface{}{
		"event":   "heartbeat",
		"emitted": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// wrapWatchEvent prepends an "event" discriminator to an entry so
// downstream jq filters can branch on type without sniffing fields.
func wrapWatchEvent(kind string, payload interface{}) map[string]interface{} {
	b, _ := json.Marshal(payload)
	var inner map[string]interface{}
	_ = json.Unmarshal(b, &inner)
	if inner == nil {
		inner = map[string]interface{}{}
	}
	inner["event"] = kind
	return inner
}

// ensure imports (url, time) stay used — NDJSON doesn't pipe
// through appendQuery so url only came in via the watcher's internal
// paths. Keep the import warning silenced here.
var _ = url.PathEscape
