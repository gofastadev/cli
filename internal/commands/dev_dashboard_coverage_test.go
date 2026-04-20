package commands

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleIndex_TemplateLoadError — force the template loader to
// return an error and expect a 500 response with the error message.
func TestHandleIndex_TemplateLoadError(t *testing.T) {
	orig := loadDashboardTemplateFn
	loadDashboardTemplateFn = func() (*template.Template, error) {
		return nil, fmt.Errorf("load failed")
	}
	t.Cleanup(func() { loadDashboardTemplateFn = orig })
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestHandleIndex_ExecuteError — the template loads but Execute
// fails. We inject a parsed template whose body references a field
// that missingkey=error would flag, OR use a template that references
// an undefined method via {{.NonExistent}}.
func TestHandleIndex_ExecuteError(t *testing.T) {
	orig := loadDashboardTemplateFn
	// Build a real parseable template whose Execute errors at runtime.
	// {{call .NoSuchFunc}} — call on a non-function value panics /
	// errors.
	tmpl, err := template.New("t").Parse(`{{call .NoSuchFunc}}`)
	require.NoError(t, err)
	loadDashboardTemplateFn = func() (*template.Template, error) { return tmpl, nil }
	t.Cleanup(func() { loadDashboardTemplateFn = orig })
	srv := &dashboardServer{}
	rec := httptest.NewRecorder()
	srv.handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestWriteSSE_MarshalFails_ViaSeam — the writeSSEMarshal seam
// returns an error; writeSSE returns early without writing.
func TestWriteSSE_MarshalFails_ViaSeam(t *testing.T) {
	orig := writeSSEMarshal
	writeSSEMarshal = func(any) ([]byte, error) { return nil, fmt.Errorf("boom") }
	t.Cleanup(func() { writeSSEMarshal = orig })
	rec := httptest.NewRecorder()
	writeSSE(rec, rec, dashboardState{})
	// No data should have been written.
	assert.Empty(t, rec.Body.String())
}

// TestHandleStream_ReceivesUpdate — subscribe to the stream, then
// trigger a refresh. The refresh broadcasts to listeners; the handler
// writes the received state via writeSSE.
func TestHandleStream_ReceivesUpdate(t *testing.T) {
	srv := &dashboardServer{
		appURL: "http://127.0.0.1:1",
		state:  dashboardState{AppPort: 8080},
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	// Start the handler in the background.
	done := make(chan struct{})
	go func() {
		srv.handleStream(rec, req)
		close(done)
	}()
	// Give the handler time to subscribe + prime.
	time.Sleep(50 * time.Millisecond)
	// Trigger a refresh — this broadcasts to the listener channel.
	srv.refresh()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	// Expect at least the primer "data:" frame + one from refresh.
	assert.Contains(t, rec.Body.String(), "data: ")
}

// TestRefresherLoop_TickFires — drive the loop with a very short
// ticker so the tick branch fires before ctx is canceled.
func TestRefresherLoop_TickFires(t *testing.T) {
	srv := &dashboardServer{appURL: "http://127.0.0.1:1"}
	orig := refresherTickInterval
	refresherTickInterval = 10 * time.Millisecond
	t.Cleanup(func() { refresherTickInterval = orig })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		srv.refresherLoop(ctx)
		close(done)
	}()
	// Let a few ticks fire.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

// TestHandleReplay_NewRequestFails — inject a failing newReplayRequest
// seam → handler returns 400.
func TestHandleReplay_NewRequestFails(t *testing.T) {
	orig := newReplayRequest
	newReplayRequest = func(context.Context, string, string, io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("build failed")
	}
	t.Cleanup(func() { newReplayRequest = orig })
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodPost, "/api/replay",
		strings.NewReader(`{"method":"GET","path":"/x"}`))
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestHandleReplay_WrongMethod — GET → 405.
func TestHandleReplay_WrongMethod(t *testing.T) {
	srv := &dashboardServer{appURL: "http://irrelevant"}
	req := httptest.NewRequest(http.MethodGet, "/api/replay", nil)
	rec := httptest.NewRecorder()
	srv.handleReplay(rec, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
