package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSource_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(path, []byte(`{"hello":"world"}`), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	data, err := resolveSource(path, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if string(data) != `{"hello":"world"}` {
		t.Errorf("got %s", data)
	}
}

func TestResolveSource_Stdin(t *testing.T) {
	data, err := resolveSource("-", strings.NewReader(`{"from":"stdin"}`))
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if string(data) != `{"from":"stdin"}` {
		t.Errorf("got %s", data)
	}
}

func TestResolveSource_URL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"from":"http"}`))
	}))
	defer srv.Close()

	data, err := resolveSource(srv.URL, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if string(data) != `{"from":"http"}` {
		t.Errorf("got %s", data)
	}
}

func TestResolveSource_URLErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := resolveSource(srv.URL, strings.NewReader("")); err == nil {
		t.Fatal("expected error for 404 status")
	}
}

// TestResolveSource_URLOversizedResponseErrors guards against an
// unbounded read from a remote spec URL: before the fix, fetchURL called
// io.ReadAll(resp.Body) with no size cap at all, so a huge or
// endlessly-streaming response could exhaust memory well within
// httpClient's 30s time budget on any reasonably fast connection.
// maxSpecBytes is shrunk here (restored after) so the test can trigger
// the real limit deterministically without actually transferring tens of
// megabytes.
func TestResolveSource_URLOversizedResponseErrors(t *testing.T) {
	old := maxSpecBytes
	maxSpecBytes = 10
	defer func() { maxSpecBytes = old }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 11)))
	}))
	defer srv.Close()

	if _, err := resolveSource(srv.URL, strings.NewReader("")); err == nil {
		t.Fatal("expected an error for a response exceeding maxSpecBytes")
	}
}

// TestResolveSource_URLResponseAtExactCapSucceeds guards the boundary:
// a response of exactly maxSpecBytes must not be mistaken for one that
// was truncated by the limiting reader.
func TestResolveSource_URLResponseAtExactCapSucceeds(t *testing.T) {
	old := maxSpecBytes
	maxSpecBytes = 10
	defer func() { maxSpecBytes = old }()

	body := strings.Repeat("x", 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	data, err := resolveSource(srv.URL, strings.NewReader(""))
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if string(data) != body {
		t.Errorf("got %q, want %q", data, body)
	}
}
