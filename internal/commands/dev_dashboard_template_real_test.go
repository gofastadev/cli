package commands

import (
	"bytes"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadDashboardTemplate caches via sync.Once. The existing tests
// substitute loadDashboardTemplateFn to inject errors/synthetic
// templates, so the REAL template.New().Funcs(...).Parse(...) body
// reports as 27.3% covered — the FuncMap closures (`deref` and `dict`)
// never fire from a render. This test resets the package vars, calls
// the real loader, and renders a tiny driver template that exercises
// both closures (including the deref nil-pointer guard).
func TestLoadDashboardTemplate_RealBodyAndFuncMap(t *testing.T) {
	// Reset the once/state so the real Once.Do body runs.
	// sync.Once contains a noCopy field so we can't snapshot+restore it;
	// instead we reset to a fresh Once. Subsequent loadDashboardTemplate
	// calls in other tests will see the now-populated cached template,
	// which is the same end state they'd reach via lazy initialization.
	dashboardTemplate = nil
	dashboardTemplateErr = nil
	dashboardTemplateOnce = sync.Once{}

	tmpl, err := loadDashboardTemplate()
	require.NoError(t, err)
	require.NotNil(t, tmpl)

	// Build a driver template under the loaded tree so it inherits the
	// registered Funcs (deref + dict). Two render passes — one with a
	// non-nil *float64 (deref returns *p), one with nil (deref returns 0).
	driver := tmpl.New("driver_for_test")
	_, err = driver.Parse(`d={{deref .Ptr}} m={{(dict "k" .V).k}}`)
	require.NoError(t, err)

	val := 3.14
	var buf bytes.Buffer
	require.NoError(t, driver.Execute(&buf, map[string]any{"Ptr": &val, "V": "ok"}))
	assert.Contains(t, buf.String(), "3.14")
	assert.Contains(t, buf.String(), "ok")

	buf.Reset()
	require.NoError(t, driver.Execute(&buf, map[string]any{"Ptr": (*float64)(nil), "V": "z"}))
	assert.Contains(t, buf.String(), "0")
}
