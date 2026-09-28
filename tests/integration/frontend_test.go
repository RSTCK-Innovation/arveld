package integration

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestControllerServesEmbeddedFrontend(t *testing.T) {
	origin, _ := startController(t, controllerConfig(t, "http://127.0.0.1:1"))
	read := func(method, path string) (int, http.Header, []byte) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, origin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := doControllerRequest(t, request)
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, response.Header, body
	}
	status, headers, index := read(http.MethodGet, "/")
	if status != http.StatusOK || !strings.HasPrefix(headers.Get("Content-Type"), "text/html") ||
		!bytes.Contains(index, []byte(`<div id="root"></div>`)) || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("frontend entry = %d %q, want built HTML without caching", status, index)
	}
	for _, path := range []string{"/settings/instance", "/agents/0198f1ad-6f2a-7a4b-8c3d-000000000001?tab=configuration", "/monitors/new/", "/index.html", "/unknown-page"} {
		status, headers, body := read(http.MethodGet, path)
		if status != http.StatusOK || !bytes.Equal(body, index) || headers.Get("Cache-Control") != "no-store" {
			t.Fatalf("SPA navigation %s = %d, want original index", path, status)
		}
		status, headers, body = read(http.MethodHead, path)
		if status != http.StatusOK || len(body) != 0 || headers.Get("Content-Length") != strconv.Itoa(len(index)) {
			t.Fatalf("HEAD %s = %d, length %s", path, status, headers.Get("Content-Length"))
		}
	}
	// The expression is a fixed test literal; a malformed pattern is a programmer error.
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+\.(?:js|css))"`).FindAllSubmatch(index, -1)
	if len(assets) == 0 {
		t.Fatal("entry must refer to built frontend assets")
	}
	for _, asset := range assets {
		path := string(asset[1])
		status, headers, body := read(http.MethodGet, path)
		contentType := headers.Get("Content-Type")
		if status != http.StatusOK || len(body) == 0 || bytes.Equal(body, index) ||
			headers.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
			headers.Get("X-Content-Type-Options") != "nosniff" ||
			(!strings.Contains(contentType, "javascript") && !strings.HasPrefix(contentType, "text/css")) {
			t.Fatalf("asset %s = %d, Content-Type %q", path, status, contentType)
		}
		status, headers, head := read(http.MethodHead, path)
		if status != http.StatusOK || len(head) != 0 || headers.Get("Content-Length") != strconv.Itoa(len(body)) {
			t.Fatalf("asset HEAD %s = %d, length %s", path, status, headers.Get("Content-Length"))
		}
	}
	status, headers, icon := read(http.MethodGet, "/favicon.svg")
	if status != http.StatusOK || len(icon) == 0 || headers.Get("Cache-Control") != "no-cache" ||
		!strings.HasPrefix(headers.Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("favicon = %d, want non-immutable SVG", status)
	}
}

func TestControllerKeepsServiceRoutesAndMissingAssetsOutsideFrontend(t *testing.T) {
	config := controllerConfig(t, "http://127.0.0.1:1")
	db := testutil.OpenDatabase(t, config.DatabasePath)
	createAdministrator(t, db)
	origin, _ := startController(t, config)
	cookie := loginController(t, origin)
	for _, test := range []struct {
		method        string
		path          string
		authenticated bool
		status        int
	}{
		{http.MethodGet, "/api/v1/agents", false, http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/agents", true, http.StatusOK},
		{http.MethodGet, "/api/v1/missing", false, http.StatusUnauthorized},
		{http.MethodGet, "/api/v1/missing", true, http.StatusNotFound},
		{http.MethodGet, "/api/v1/auth/missing", false, http.StatusNotFound},
		{http.MethodGet, "/api", false, http.StatusNotFound},
		{http.MethodGet, "/api/v1", false, http.StatusNotFound},
		{http.MethodGet, "/api/v2/missing", false, http.StatusNotFound},
		{http.MethodGet, "/API/v1/agents", false, http.StatusNotFound},
		{http.MethodGet, "/v1/missing", false, http.StatusNotFound},
		{http.MethodPost, "/v1/opamp", false, http.StatusUnauthorized},
		{http.MethodPost, "/v1/otlp/v1/metrics", false, http.StatusUnauthorized},
		{http.MethodGet, "/v1/otlp/v1/metrics", false, http.StatusMethodNotAllowed},
		{http.MethodGet, "/healthz", false, http.StatusOK},
		{http.MethodGet, "/readyz", false, http.StatusServiceUnavailable},
		{http.MethodGet, "/healthz/missing", false, http.StatusNotFound},
		{http.MethodGet, "/READYZ", false, http.StatusNotFound},
		{http.MethodPost, "/settings", false, http.StatusMethodNotAllowed},
		{http.MethodGet, "/assets/missing.js", false, http.StatusNotFound},
		{http.MethodGet, "/assets/missing", false, http.StatusNotFound},
		{http.MethodHead, "/assets/missing.js", false, http.StatusNotFound},
		{http.MethodGet, "/assets/", false, http.StatusNotFound},
		{http.MethodGet, "/assets", false, http.StatusNotFound},
		{http.MethodGet, "/missing.svg", false, http.StatusNotFound},
		{http.MethodGet, "/src/main.tsx", false, http.StatusNotFound},
		{http.MethodGet, "/package.json", false, http.StatusNotFound},
		{http.MethodGet, "/arveld.yml", false, http.StatusNotFound},
		{http.MethodGet, "/%2e%2e/README.md", false, http.StatusNotFound},
		{http.MethodGet, "/settings//instance", false, http.StatusNotFound},
		{http.MethodGet, "/settings/%5cinstance", false, http.StatusNotFound},
	} {
		t.Run(test.method+" "+test.path+" "+http.StatusText(test.status), func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), test.method, origin+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.authenticated {
				request.AddCookie(cookie)
			}
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := response.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.status || bytes.Contains(body, []byte(`<div id="root"></div>`)) || response.Header.Get("Location") != "" {
				t.Fatalf("response = %d %q, want %d without frontend or redirect", response.StatusCode, body, test.status)
			}
			if test.method == http.MethodHead && len(body) != 0 {
				t.Fatal("HEAD returned a body")
			}
			if test.path == "/settings" && response.Header.Get("Allow") != "GET, HEAD" {
				t.Fatal("frontend must advertise GET, HEAD")
			}
		})
	}
}
