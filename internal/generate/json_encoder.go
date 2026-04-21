package generate

import (
	"encoding/json"
	"io"
)

// jsonEncoder is a tiny helper that keeps JSON emission out of the
// commands.go body. It exists because internal/generate cannot import
// internal/cliout (they're peers in internal/ and crossing that
// boundary risks import cycles in tests). Wrapping the few json lines
// in a named type keeps the call site readable.
type jsonEncoder struct{}

// WriteTo marshals v to w as a single-line JSON document. Write errors
// are swallowed — stdout going away mid-command is not actionable.
func (jsonEncoder) WriteTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
