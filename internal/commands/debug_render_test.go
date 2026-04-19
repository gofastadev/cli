package commands

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTruncate — at or below width: unchanged; over width: trailing
// ellipsis, total length == width.
func TestTruncate(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  string
	}{
		{"", 10, ""},
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"longer than cap", 10, "longer th…"},
		{"anything", 1, "…"},
		{"anything", 0, "anything"}, // width 0 → passthrough
	}
	for _, c := range cases {
		assert.Equal(t, c.want, truncate(c.in, c.width),
			"truncate(%q, %d)", c.in, c.width)
	}
}

// TestFormatClock — HH:MM:SS.mmm format matches the dashboard.
func TestFormatClock(t *testing.T) {
	tm := time.Date(2026, 4, 19, 15, 34, 12, 104_000_000, time.UTC)
	assert.Equal(t, "15:34:12.104", formatClock(tm))
}

// TestFormatMS — right-pads to 5 cols for column alignment.
func TestFormatMS(t *testing.T) {
	assert.Equal(t, "    5 ms", formatMS(5))
	assert.Equal(t, "   42 ms", formatMS(42))
	assert.Equal(t, "  612 ms", formatMS(612))
}

// TestStatusPill — each class gets a distinct color wrap; unknown
// code passes through raw.
func TestStatusPill(t *testing.T) {
	// Strip ANSI to assert on the embedded code — the codes are tested
	// in termcolor itself; here we just verify the branching.
	strip := stripANSI
	assert.Equal(t, "200", strip(statusPill(200)))
	assert.Equal(t, "302", strip(statusPill(302)))
	assert.Equal(t, "404", strip(statusPill(404)))
	assert.Equal(t, "500", strip(statusPill(500)))
	assert.Equal(t, "0", strip(statusPill(0)))
}

// TestMethodPill — every branch of the switch.
func TestMethodPill(t *testing.T) {
	for _, m := range []string{"GET", "POST", "PATCH", "DELETE", "HEAD"} {
		assert.Equal(t, m, stripANSI(methodPill(m)),
			"method=%s should round-trip", m)
	}
	// Unknown method falls through unchanged.
	assert.Equal(t, "CUSTOM", stripANSI(methodPill("CUSTOM")))
}

// TestLevelPill — covers every documented level + default.
func TestLevelPill(t *testing.T) {
	for _, lvl := range []string{"DEBUG", "INFO", "WARN", "WARNING", "ERROR", "TRACE"} {
		assert.Equal(t, lvl, stripANSI(levelPill(lvl)))
	}
}

// TestTraceIDShort — long IDs truncated with ellipsis; short IDs
// passed through.
func TestTraceIDShort(t *testing.T) {
	assert.Equal(t, "", traceIDShort(""))
	assert.Equal(t, "short", traceIDShort("short"))
	assert.Equal(t, "12345678", traceIDShort("12345678"))
	assert.Equal(t, "12345678…", traceIDShort("1234567890abcdef"))
}

// TestPrintFilterSummary — renders count + filter string to the
// writer. ANSI-stripped output is asserted.
func TestPrintFilterSummary_WithFilters(t *testing.T) {
	var buf bytes.Buffer
	printFilterSummary(&buf, 3, 10, map[string]string{
		"method":      "POST",
		"slower-than": "100ms",
		"ignored":     "",
	})
	plain := stripANSI(buf.String())
	assert.Contains(t, plain, "Showing 3 of 10 entries")
	assert.Contains(t, plain, "filters:")
	assert.Contains(t, plain, "method=POST")
	assert.Contains(t, plain, "slower-than=100ms")
	assert.NotContains(t, plain, "ignored=")
}

// TestPrintFilterSummary_NoFilters — no filter clause when everything
// is empty.
func TestPrintFilterSummary_NoFilters(t *testing.T) {
	var buf bytes.Buffer
	printFilterSummary(&buf, 5, 5, map[string]string{"a": "", "b": ""})
	plain := stripANSI(buf.String())
	assert.Contains(t, plain, "Showing 5 of 5 entries")
	assert.NotContains(t, plain, "filters:")
}

// TestNewTabWriter — smoke check that it writes tab-aligned output.
func TestNewTabWriter(t *testing.T) {
	var buf bytes.Buffer
	tw := newTabWriter(&buf)
	_, _ = tw.Write([]byte("A\tB\n"))
	_, _ = tw.Write([]byte("longer\tvalue\n"))
	require.NoError(t, tw.Flush())
	// Every row must be present and the second column must line up.
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 2)
	// tabwriter pads column 1 to the max width; both lines should
	// therefore have the same index for column 2.
	idx0 := strings.Index(lines[0], "B")
	idx1 := strings.Index(lines[1], "value")
	assert.Equal(t, idx0, idx1, "columns did not align: %q", out)
}

// ── Waterfall ─────────────────────────────────────────────────────────

// TestBuildWaterfallTree_MultipleRoots — spans with no parent ID
// become separate roots.
func TestBuildWaterfallTree_MultipleRoots(t *testing.T) {
	spans := []scrapedSpan{
		{SpanID: "r1", Name: "rootA"},
		{SpanID: "r2", Name: "rootB"},
	}
	tree := buildWaterfallTree(spans)
	assert.Len(t, tree, 2)
}

// TestBuildWaterfallTree_DanglingParent — a span pointing at a
// missing parent should still become a root (defensive fallback).
func TestBuildWaterfallTree_DanglingParent(t *testing.T) {
	spans := []scrapedSpan{
		{SpanID: "a", ParentID: "nonexistent", Name: "orphan"},
	}
	tree := buildWaterfallTree(spans)
	require.Len(t, tree, 1)
	assert.Equal(t, "orphan", tree[0].Name)
}

// TestRenderWaterfall_WithErrorSpan — spans with Status="error"
// get red styling (branch coverage for the status switch).
func TestRenderWaterfall_WithErrorSpan(t *testing.T) {
	spans := []scrapedSpan{
		{SpanID: "r", Name: "root", OffsetMS: 0, DurationMS: 10, Status: "error"},
	}
	var buf bytes.Buffer
	renderWaterfall(&buf, 10, spans, false)
	out := buf.String()
	assert.Contains(t, out, "root")
}

// TestWaterfallBar_ZeroTotal — when total=0 we still render a bar
// without dividing by zero.
func TestWaterfallBar_ZeroTotal(t *testing.T) {
	got := waterfallBar(0, 0, 0)
	assert.NotEmpty(t, got)
}

// TestWaterfallBar_TinyDurationMinWidth — a zero-duration span still
// renders at least one cell so it's visible.
func TestWaterfallBar_TinyDurationMinWidth(t *testing.T) {
	got := waterfallBar(0, 0, 1000)
	// Should contain at least one filled cell (U+2588).
	assert.Contains(t, stripANSI(got), "█")
}

// TestWaterfallBar_ClampsOverflow — when offset+duration would exceed
// the track width, the bar is clipped rather than overflowing.
func TestWaterfallBar_ClampsOverflow(t *testing.T) {
	got := waterfallBar(90, 50, 100)
	plain := stripANSI(got)
	// Bar track width is waterfallBarWidth + 2 bracket chars.
	assert.Equal(t, waterfallBarWidth+2, len([]rune(plain)))
}
