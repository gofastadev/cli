package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gofastadev/cli/internal/termcolor"
)

// ─────────────────────────────────────────────────────────────────────
// Dev pipeline events.
//
// One event type per pipeline step. When --json is set, every event is
// emitted to stdout as a newline-delimited JSON object so agents / CI
// tooling can branch on facts. When --json is NOT set, events render
// through termcolor as human-friendly status lines (identical visual
// contract the existing runDev had, just with more stages covered).
// ─────────────────────────────────────────────────────────────────────

// devEvent is the union type for every event the dev pipeline can emit.
// Exactly one of the typed fields should be set; `Event` is the
// discriminator. Producing a single struct (rather than separate types
// per event) lets the JSON consumer decode everything with one schema.
type devEvent struct {
	Event string `json:"event"`

	// preflight
	Docker  string `json:"docker,omitempty"`
	Compose string `json:"compose,omitempty"`

	// service
	Name       string `json:"name,omitempty"`
	State      string `json:"state,omitempty"`
	Health     string `json:"health,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`

	// migrate
	Applied int `json:"applied,omitempty"`

	// air
	Port int               `json:"port,omitempty"`
	URLs map[string]string `json:"urls,omitempty"`

	// shutdown
	Teardown string `json:"teardown,omitempty"`
	Exit     int    `json:"exit,omitempty"`

	// universal
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

// devEmitter is what the pipeline calls to report progress. The human
// and JSON variants implement this so runDev never branches on output
// format — it just emits events and the emitter decides how to render.
type devEmitter interface {
	Preflight(docker, compose string)
	ServiceStart(name string)
	ServiceHealthy(name string, elapsed time.Duration)
	ServiceUnhealthy(name, reason string)
	MigrateOK(applied int)
	MigrateSkipped(reason string)
	Air(port int, urls map[string]string)
	Shutdown(teardown string, exit int)
	Info(msg string)
	Warn(msg string)
}

// newDevEmitter picks the JSON or human emitter based on the --json
// flag mirrored into cliout. Structured mode is attached to os.Stdout
// so cobra's built-in stdout capture works for tests.
func newDevEmitter(jsonMode bool) devEmitter {
	if jsonMode {
		return &jsonEmitter{out: os.Stdout}
	}
	return &humanEmitter{}
}

// ── JSON mode ─────────────────────────────────────────────────────────

type jsonEmitter struct {
	out io.Writer
	// marshal is a seam so tests can inject a failing marshaler to
	// exercise the "json.Marshal returned error" branch. Production
	// always uses json.Marshal via jsonMarshal.
	marshal func(any) ([]byte, error)
}

// jsonMarshal is the default marshaler used by jsonEmitter. Indirected
// through this package-level var so tests could swap it out; kept as a
// package-level function rather than directly assigning json.Marshal so
// the linter's unused-import rules stay happy.
var jsonMarshal = json.Marshal

// emit marshals an event to JSON and writes it as a single line.
func (e *jsonEmitter) emit(ev devEvent) {
	marshal := e.marshal
	if marshal == nil {
		marshal = jsonMarshal
	}
	b, err := marshal(ev)
	if err != nil {
		// Marshal of a plain struct cannot fail unless a field contains
		// a non-marshalable type. Fall back to a bare error event so
		// the stream still parses.
		_, _ = fmt.Fprintf(e.out, `{"event":"error","message":%q}`+"\n", err.Error())
		return
	}
	_, _ = e.out.Write(b)
	_, _ = e.out.Write([]byte{'\n'})
}

// Preflight — docker + compose versions detected during preflight.
func (e *jsonEmitter) Preflight(docker, compose string) {
	e.emit(devEvent{Event: "preflight", Status: "ok", Docker: docker, Compose: compose})
}

// ServiceStart — a compose service has begun starting.
func (e *jsonEmitter) ServiceStart(name string) {
	e.emit(devEvent{Event: "service", Name: name, Status: "starting"})
}

// ServiceHealthy — a compose service reported healthy/running.
func (e *jsonEmitter) ServiceHealthy(name string, elapsed time.Duration) {
	e.emit(devEvent{
		Event:      "service",
		Name:       name,
		Status:     "healthy",
		DurationMS: elapsed.Milliseconds(),
	})
}

// ServiceUnhealthy — a compose service failed to become healthy.
func (e *jsonEmitter) ServiceUnhealthy(name, reason string) {
	e.emit(devEvent{Event: "service", Name: name, Status: "unhealthy", Message: reason})
}

// MigrateOK — `migrate up` succeeded (possibly with zero migrations applied).
func (e *jsonEmitter) MigrateOK(applied int) {
	e.emit(devEvent{Event: "migrate", Status: "ok", Applied: applied})
}

// MigrateSkipped — migrations were skipped (disabled or failed non-fatally).
func (e *jsonEmitter) MigrateSkipped(reason string) {
	e.emit(devEvent{Event: "migrate", Status: "skipped", Message: reason})
}

// Air — Air launched successfully; emits the URL set for the running app.
func (e *jsonEmitter) Air(port int, urls map[string]string) {
	e.emit(devEvent{Event: "air", Status: "running", Port: port, URLs: urls})
}

// Shutdown — pipeline exited; reports teardown result and exit code.
func (e *jsonEmitter) Shutdown(teardown string, exit int) {
	e.emit(devEvent{Event: "shutdown", Teardown: teardown, Exit: exit})
}

// Info — generic progress line, emitted as an "info" event.
func (e *jsonEmitter) Info(msg string) {
	e.emit(devEvent{Event: "info", Message: msg})
}

// Warn — generic non-fatal warning, emitted as a "warn" event.
func (e *jsonEmitter) Warn(msg string) {
	e.emit(devEvent{Event: "warn", Message: msg})
}

// ── Human mode ────────────────────────────────────────────────────────

type humanEmitter struct{}

// Preflight prints a single status line with detected docker / compose versions.
func (h *humanEmitter) Preflight(docker, compose string) {
	termcolor.PrintStep("✓ docker %s · compose %s", docker, compose)
}

// ServiceStart prints a "starting" line for a compose service.
func (h *humanEmitter) ServiceStart(name string) {
	termcolor.PrintStep("→ starting %s", name)
}

// ServiceHealthy prints a "healthy" line with the elapsed startup time.
func (h *humanEmitter) ServiceHealthy(name string, elapsed time.Duration) {
	termcolor.PrintStep("✓ %s healthy (%s)", name, elapsed.Round(100*time.Millisecond))
}

// ServiceUnhealthy prints a warning for a service that never became healthy.
func (h *humanEmitter) ServiceUnhealthy(name, reason string) {
	termcolor.PrintWarn("✗ %s unhealthy: %s", name, reason)
}

// MigrateOK prints "migrations applied" or "migrations up to date".
func (h *humanEmitter) MigrateOK(applied int) {
	if applied > 0 {
		termcolor.PrintStep("✓ migrations applied (%d)", applied)
	} else {
		termcolor.PrintStep("✓ migrations up to date")
	}
}

// MigrateSkipped prints the reason migrations were skipped.
func (h *humanEmitter) MigrateSkipped(reason string) {
	termcolor.PrintWarn("migrations skipped: %s", reason)
}

// Air prints the post-start URL banner for the running app.
func (h *humanEmitter) Air(port int, urls map[string]string) {
	fmt.Println()
	termcolor.PrintStep("🚀 Air running on :%d", port)
	for label, url := range urls {
		fmt.Printf("   %s    %s\n", termcolor.CDim(label+":"), termcolor.CBlue(url))
	}
	fmt.Println()
}

// Shutdown prints the teardown status line at pipeline exit.
func (h *humanEmitter) Shutdown(teardown string, _ int) {
	termcolor.PrintStep("shutdown — services %s", teardown)
}

// Info prints a generic progress line.
func (h *humanEmitter) Info(msg string) {
	termcolor.PrintStep("%s", msg)
}

// Warn prints a generic non-fatal warning.
func (h *humanEmitter) Warn(msg string) {
	termcolor.PrintWarn("%s", msg)
}
