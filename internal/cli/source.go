package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// maxSpecBytes caps how large a remote spec fetchURL will accept. Real
// OpenAPI/Swagger documents are practically always well under a few MB
// even for large APIs, so this reflects a generous safety margin, not an
// expected legitimate size — it exists to bound memory use against a
// huge or endlessly-streaming response (accidental or adversarial)
// rather than the 30s httpClient.Timeout above, which bounds time, not
// bytes: on a fast connection, tens of GB can transfer well within 30
// seconds. A var (not a const) so tests can shrink it for a fast,
// deterministic repro instead of actually generating tens of MB.
var maxSpecBytes int64 = 50 * 1024 * 1024

// resolveSource reads the spec bytes named by source: an http(s):// URL is
// fetched, "-" reads from stdin, anything else is treated as a local file
// path.
func resolveSource(source string, stdin io.Reader) ([]byte, error) {
	switch {
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		return fetchURL(source)
	case source == "-":
		return io.ReadAll(stdin)
	default:
		return os.ReadFile(source)
	}
}

func fetchURL(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}
	// Read one byte past the cap so a response of exactly maxSpecBytes
	// isn't mistaken for one that was truncated, while a larger response
	// is still bounded (io.ReadAll never buffers more than
	// maxSpecBytes+1 bytes here, regardless of how much more the server
	// tries to send).
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if int64(len(body)) > maxSpecBytes {
		return nil, fmt.Errorf("fetch %s: response exceeds %d bytes", url, maxSpecBytes)
	}
	return body, nil
}
