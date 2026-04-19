package commands

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gofastadev/cli/internal/termcolor"
)

// newTabWriter wraps text/tabwriter with the CLI's shared defaults so
// every debug command emits aligned columns with a consistent look.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
}

// truncate clips s to width runes, appending an ellipsis when it would
// otherwise exceed the limit. Used to keep long paths / SQL from
// blowing the layout.
func truncate(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return s[:width-1] + "…"
}

// formatClock renders a time in HH:MM:SS.mmm form (matches the
// dashboard's presentation). Used consistently across every list
// command so human output reads the same everywhere.
func formatClock(t time.Time) string {
	return t.Format("15:04:05.000")
}

// formatMS adds a "ms" suffix and right-pads to 6 cols so the duration
// column aligns across rows.
func formatMS(ms int64) string {
	return fmt.Sprintf("%5d ms", ms)
}

// statusPill returns a colored string for an HTTP status code. 2xx =
// green, 3xx = cyan, 4xx = yellow, 5xx = red. Color automatically
// disables on non-TTY stdout — see termcolor.Enabled.
func statusPill(status int) string {
	s := fmt.Sprintf("%d", status)
	switch {
	case status >= 200 && status < 300:
		return termcolor.CGreen(s)
	case status >= 300 && status < 400:
		return termcolor.CBlue(s)
	case status >= 400 && status < 500:
		return termcolor.CYellow(s)
	case status >= 500:
		return termcolor.CRed(s)
	default:
		return s
	}
}

// methodPill colors an HTTP method for the human-readable column. Same
// palette the dashboard uses so developers see one consistent look
// across surfaces.
func methodPill(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return termcolor.CBrand(method)
	case "POST":
		return termcolor.CGreen(method)
	case "PATCH":
		return termcolor.CYellow(method)
	case "DELETE":
		return termcolor.CRed(method)
	default:
		return method
	}
}

// levelPill colors a log level. INFO = green, WARN = yellow, ERROR =
// red, anything else (DEBUG / TRACE) = dim.
func levelPill(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR":
		return termcolor.CRed(level)
	case "WARN", "WARNING":
		return termcolor.CYellow(level)
	case "INFO":
		return termcolor.CGreen(level)
	default:
		return termcolor.CDim(level)
	}
}

// traceIDShort returns the first 8 characters of a trace ID for human
// display. Full IDs stay in JSON output; trimming is purely visual.
func traceIDShort(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "…"
}

// printFilterSummary appends a dim footnote summarizing applied filters
// and the displayed vs total count. Humans scanning the output see at a
// glance that a filter is active.
func printFilterSummary(w io.Writer, shown, total int, filters map[string]string) {
	pairs := make([]string, 0, len(filters))
	for k, v := range filters {
		if v == "" {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	filterStr := ""
	if len(pairs) > 0 {
		filterStr = " · filters: " + strings.Join(pairs, ", ")
	}
	_, _ = fmt.Fprintln(w, termcolor.CDim(
		fmt.Sprintf("\nShowing %d of %d entries%s", shown, total, filterStr),
	))
}

// ── Waterfall renderer ────────────────────────────────────────────────
//
// Renders a trace's spans as an indented tree with ASCII bars scaled
// to the trace's total duration. Each row shows:
//
//   <duration>  <bar>  <tree-glyph><name> <kind>
//
// depthByID is computed up-front by walking parent-child relationships
// so tree glyphs render correctly regardless of span order in the
// input.

const waterfallBarWidth = 40

// waterfallRenderNode is a node in the waterfall tree. Built from a
// flat list of spans via parent-ID indexing.
type waterfallRenderNode struct {
	SpanID     string
	Name       string
	Kind       string
	OffsetMS   int64
	DurationMS int64
	Status     string
	Stack      []string
	Children   []*waterfallRenderNode
}

// renderWaterfall writes a human-readable trace waterfall to w.
// totalMS is the trace's root duration in ms; spans is the flat
// ordered list as returned by /debug/traces/{id}. When withStacks is
// true, each span's captured call stack is printed below it.
func renderWaterfall(w io.Writer, totalMS int64, spans []scrapedSpan, withStacks bool) {
	if len(spans) == 0 {
		_, _ = fmt.Fprintln(w, termcolor.CDim("  (no spans)"))
		return
	}
	tree := buildWaterfallTree(spans)
	for i, root := range tree {
		renderWaterfallNode(w, root, "", i == len(tree)-1, totalMS, withStacks)
	}
}

// buildWaterfallTree indexes spans by ID then threads children onto
// their parents. Returns the root forest (usually one element — a
// well-formed trace has one root).
func buildWaterfallTree(spans []scrapedSpan) []*waterfallRenderNode {
	nodes := make(map[string]*waterfallRenderNode, len(spans))
	for _, s := range spans {
		nodes[s.SpanID] = &waterfallRenderNode{
			SpanID:     s.SpanID,
			Name:       s.Name,
			Kind:       s.Kind,
			OffsetMS:   s.OffsetMS,
			DurationMS: s.DurationMS,
			Status:     s.Status,
			Stack:      s.Stack,
		}
	}
	var roots []*waterfallRenderNode
	for _, s := range spans {
		n := nodes[s.SpanID]
		if s.ParentID == "" || nodes[s.ParentID] == nil {
			roots = append(roots, n)
			continue
		}
		parent := nodes[s.ParentID]
		parent.Children = append(parent.Children, n)
	}
	return roots
}

// renderWaterfallNode recursively prints one span + its subtree. prefix
// accumulates the tree glyphs from ancestors; isLast controls whether
// to use the trailing-branch glyph.
func renderWaterfallNode(
	w io.Writer, n *waterfallRenderNode,
	prefix string, isLast bool,
	totalMS int64, withStacks bool,
) {
	glyph := "├─ "
	childPrefix := prefix + "│  "
	if isLast {
		glyph = "└─ "
		childPrefix = prefix + "   "
	}
	if prefix == "" && isLast && len(n.Children) == 0 {
		glyph = ""
		childPrefix = ""
	}

	bar := waterfallBar(n.OffsetMS, n.DurationMS, totalMS)
	name := n.Name
	if n.Status == "error" {
		name = termcolor.CRed(name)
	}
	kindSuffix := ""
	if n.Kind != "" && n.Kind != "SPAN_KIND_UNSPECIFIED" {
		kindSuffix = " " + termcolor.CDim("("+n.Kind+")")
	}

	_, _ = fmt.Fprintf(
		w, "%7d ms  %s  %s%s%s%s\n",
		n.DurationMS, bar, prefix, glyph, name, kindSuffix,
	)

	if withStacks && len(n.Stack) > 0 {
		stackPrefix := childPrefix + "  "
		for _, frame := range n.Stack {
			_, _ = fmt.Fprintln(w, termcolor.CDim(stackPrefix+frame))
		}
	}

	for i, c := range n.Children {
		renderWaterfallNode(w, c, childPrefix, i == len(n.Children)-1, totalMS, withStacks)
	}
}

// waterfallBar builds the scaled ASCII bar for one span. offsetMS is
// relative to the trace root's start; the bar is waterfallBarWidth
// characters wide and fills only the cells that fall inside the span's
// time range.
func waterfallBar(offsetMS, durationMS, totalMS int64) string {
	if totalMS <= 0 {
		totalMS = 1
	}
	startCell := int(float64(offsetMS) / float64(totalMS) * float64(waterfallBarWidth))
	if startCell < 0 {
		startCell = 0
	}
	widthCells := int(float64(durationMS) / float64(totalMS) * float64(waterfallBarWidth))
	if widthCells < 1 {
		widthCells = 1
	}
	if startCell+widthCells > waterfallBarWidth {
		widthCells = waterfallBarWidth - startCell
	}

	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < waterfallBarWidth; i++ {
		if i >= startCell && i < startCell+widthCells {
			b.WriteString(termcolor.CBrand("█"))
		} else {
			b.WriteByte(' ')
		}
	}
	b.WriteByte(']')
	return b.String()
}
