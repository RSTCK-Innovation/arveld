package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/RSTCK-Innovation/arveld/tests/testutil"
)

func TestHEADAndMethodFallbackPreserveHTTPContract(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	for _, route := range []struct {
		path, emptyBody, allow string
	}{
		{"/api/v1/agents", "{\"agents\":[]}\n", "GET, HEAD"},
		{"/api/v1/agentkeys", "{\"keys\":[]}\n", "GET, HEAD, POST"},
	} {
		t.Run(route.path, func(t *testing.T) {
			for _, method := range []string{http.MethodHead, http.MethodPatch} {
				request, err := http.NewRequestWithContext(t.Context(), method, server.URL+route.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				request.AddCookie(cookie)
				response, err := server.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if err := response.Body.Close(); err != nil {
					t.Fatal(err)
				}
				allowed := response.Header.Values("Allow")
				slices.Sort(allowed)
				if method == http.MethodHead {
					if response.StatusCode != http.StatusOK || len(body) != 0 || response.ContentLength != int64(len(route.emptyBody)) {
						t.Fatalf("HEAD = %d, length %d", response.StatusCode, response.ContentLength)
					}
				} else if response.StatusCode != http.StatusMethodNotAllowed || strings.Join(allowed, ", ") != route.allow {
					t.Fatalf("method fallback = %d, Allow %q", response.StatusCode, allowed)
				}
			}
		})
	}
}

func TestHTTPServerDoesNotRepairWritePaths(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	requestStatus := func(method, path, body string) int {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatalf("send request: %v", err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatalf("close response: %v", err)
		}
		return response.StatusCode
	}

	if status := requestStatus(http.MethodGet, "/api/v1/auth/session", ""); status != http.StatusOK {
		t.Fatalf("session before logout = %d, want 200", status)
	}
	if status := requestStatus(http.MethodPut, "/api/v1/account//profile", `{"name":"Robin","email":"robin@example.com"}`); status != http.StatusNotFound {
		t.Fatalf("profile write on an undeclared path = %d, want 404", status)
	}
	if status := requestStatus(http.MethodPost, "/api/v1/auth//logout", ""); status != http.StatusNotFound {
		t.Fatalf("logout on an undeclared path = %d, want 404", status)
	}
	if status := requestStatus(http.MethodGet, "/api/v1/auth/session", ""); status != http.StatusOK {
		t.Fatalf("session after rejected logout = %d, want 200", status)
	}
	if status := requestStatus(http.MethodPost, "/api/v1/auth/logout", ""); status != http.StatusNoContent {
		t.Fatalf("logout on the declared path = %d, want 204", status)
	}
	if status := requestStatus(http.MethodGet, "/api/v1/auth/session", ""); status != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", status)
	}
}

func TestHTTPServerMatchesDeclaredPaths(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	handler := newAccountHandler(db)
	cookie := login(t, handler, "a long password for testing")
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/agents", 200},
		{"GET", "/api/v1/auth", 404},
		{"GET", "/api/v1/account", 404},
		{"GET", "/api/v1", 404},
		{"CONNECT", "/api/v1/auth", 404},
		{"CONNECT", "/api/v1//agents", 404},
		{"POST", "/api/v1/auth//login?next=agents", 404},
		{"GET", "/api/v1/agents/../agents", 404},
		{"GET", "//outside.example/api/../path?next=https://outside.example", 404},
		{"GET", "/%2Foutside.example//path", 404},
		{"GET", "/api/v1/agents/", 404},
		{"POST", "/api/v1/agent-keys", 404},
		{"POST", "/api/v1/agents", 405},
		{"GET", "/HEALTHZ", 404},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), test.method, test.path, nil)
			request.AddCookie(cookie)
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Location") != "" {
				t.Fatalf("routing = %d, Location %q; want %d without a redirect", response.Code, response.Header().Get("Location"), test.status)
			}
		})
	}
}

func TestHTTPServerAuthenticatesBeforeManagementFallbacks(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")

	for _, route := range []struct {
		method, path, allow string
		status              int
	}{
		{http.MethodPatch, "/api/v1/apikeys", "GET, HEAD, POST", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v1/agentkeys", "GET, HEAD, POST", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v1/%61gentkeys", "", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/agentkeys/unknown", "DELETE", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v1/agentkeys/unknown/details", "", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/account/profile", "PUT", http.StatusMethodNotAllowed},
		{http.MethodPatch, "/api/v1/account/unknown", "", http.StatusNotFound},
		{http.MethodPatch, "/api/v1/apikeys/unknown/details", "", http.StatusNotFound},
		{http.MethodPost, "/api/v1/agents", "GET, HEAD", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/unknown", "", http.StatusNotFound},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			for _, identity := range []struct {
				name, origin, allow string
				cookie              *http.Cookie
				status              int
			}{
				{"anonymous", "", "", nil, http.StatusUnauthorized},
				{"cross origin", "https://another.example", "", cookie, http.StatusForbidden},
				{"authorized", "", route.allow, cookie, route.status},
			} {
				t.Run(identity.name, func(t *testing.T) {
					request := httptest.NewRequestWithContext(t.Context(), route.method, "https://arveld.example"+route.path, nil)
					if identity.cookie != nil {
						request.AddCookie(identity.cookie)
					}
					if identity.origin != "" {
						request.Header.Set("Origin", identity.origin)
					}
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					allowed := response.Header().Values("Allow")
					slices.Sort(allowed)
					if response.Code != identity.status || strings.Join(allowed, ", ") != identity.allow {
						t.Fatalf("response = %d, Allow %q; want %d, %q", response.Code, allowed, identity.status, identity.allow)
					}
					if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Set-Cookie") != "" {
						t.Fatal("management fallbacks must not be cached or renew the session cookie")
					}
				})
			}
		})
	}
}

func TestHTTPServerAuthenticatesEncodedPaths(t *testing.T) {
	db := testutil.OpenDatabase(t, filepath.Join(t.TempDir(), "arveld.db"))
	createAdministrator(t, db)
	server := newAccountHandler(db)
	cookie := login(t, server, "a long password for testing")

	for _, route := range []struct {
		method, path, cache      string
		anonymous, authenticated int
	}{
		{http.MethodGet, "/api%2Fv1/agents", "no-store", 401, 404},
		{http.MethodGet, "/api/v1%2fagents", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/auth%2Fsetup", "", 404, 404},
		{http.MethodGet, "/api/v1/account%2Fprofile", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/apikeys%2Funknown", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/agents/not%2Fan%2Fid/config/status", "no-store", 401, 400},
		{http.MethodGet, "/api/v1/agents/%2E%2E/config/status", "no-store", 401, 400},
		{http.MethodGet, "/api/v1/%2561gents", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/%61gents", "no-store", 401, 404},
		{http.MethodPatch, "/api/v1/%61gents", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/%61uth/setup", "", 404, 404},
		{http.MethodGet, "/api/v1/%61uth?next=%2Fagents", "no-store", 401, 404},
		{http.MethodGet, "/api/v1/%61ccount", "no-store", 401, 404},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			for _, authenticated := range []bool{false, true} {
				name, status := "anonymous", route.anonymous
				if authenticated {
					name, status = "authenticated", route.authenticated
				}
				t.Run(name, func(t *testing.T) {
					request := httptest.NewRequestWithContext(t.Context(), route.method, "https://arveld.example"+route.path, nil)
					if authenticated {
						request.AddCookie(cookie)
					}
					response := httptest.NewRecorder()
					server.ServeHTTP(response, request)
					if response.Code != status || response.Header().Get("Location") != "" || len(response.Header().Values("Allow")) != 0 {
						t.Fatalf("response = %d, headers %v; want %d without redirect or method headers", response.Code, response.Header(), status)
					}
					if response.Header().Get("Cache-Control") != route.cache || response.Header().Get("Set-Cookie") != "" {
						t.Fatalf("unexpected cache or session headers: %v", response.Header())
					}
				})
			}
		})
	}
}
