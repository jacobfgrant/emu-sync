package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jacobfgrant/emu-sync/internal/config"
)

const testHost = "127.0.0.1:43210"

// guardedServer returns a sync-capable web server with a token and host set.
func guardedServer(t *testing.T) *webServer {
	t.Helper()
	ws, _ := setupSyncWebServer(t)
	ws.token = "test-token"
	ws.host = testHost
	return ws
}

func serve(ws *webServer, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	ws.routes().ServeHTTP(rec, req)
	return rec
}

func newGuardRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = testHost
	return req
}

func TestGuardRejectsAPIWithoutToken(t *testing.T) {
	ws := guardedServer(t)

	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/systems"},
		{"POST", "/api/save"},
		{"POST", "/api/sync"},
		{"POST", "/api/exit"},
		{"POST", "/api/verify"},
		{"POST", "/api/preview"},
		{"GET", "/api/sync/status"},
		{"GET", "/api/sync/events"},
		{"GET", "/api/wait"},
	} {
		rec := serve(ws, newGuardRequest(tc.method, tc.path, ""))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without token: got %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestGuardBlocksCrossSiteSync(t *testing.T) {
	ws := guardedServer(t)

	// A cross-site form or fetch can send text/plain without a preflight,
	// and json.Decoder ignores Content-Type, so the token is what stops it.
	req := newGuardRequest("POST", "/api/sync", `{"selections":{},"delete":true}`)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "https://evil.example")
	rec := serve(ws, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", rec.Code)
	}
	if ws.cfg.Sync.Delete {
		t.Error("rejected request should not change delete setting")
	}
	if _, err := os.Stat(ws.cfgPath); !os.IsNotExist(err) {
		t.Error("rejected request should not write the config")
	}
}

func TestGuardRejectsWrongToken(t *testing.T) {
	ws := guardedServer(t)
	req := newGuardRequest("GET", "/api/sync/status", "")
	req.Header.Set(tokenHeader, "wrong-token")

	if rec := serve(ws, req); rec.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", rec.Code)
	}
}

func TestGuardAcceptsTokenHeader(t *testing.T) {
	ws := guardedServer(t)
	req := newGuardRequest("GET", "/api/sync/status", "")
	req.Header.Set(tokenHeader, "test-token")

	if rec := serve(ws, req); rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestGuardAcceptsTokenQueryParam(t *testing.T) {
	ws := guardedServer(t)
	req := newGuardRequest("GET", "/api/sync/status?token=test-token", "")

	if rec := serve(ws, req); rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}

func TestGuardEmptyServerTokenRejectsEverything(t *testing.T) {
	ws := guardedServer(t)
	ws.token = ""
	req := newGuardRequest("GET", "/api/sync/status", "")
	req.Header.Set(tokenHeader, "")

	if rec := serve(ws, req); rec.Code != http.StatusForbidden {
		t.Errorf("got %d, want 403", rec.Code)
	}
}

func TestGuardRejectsForeignHost(t *testing.T) {
	ws := guardedServer(t)

	// DNS rebinding: the browser connects to 127.0.0.1 but sends the
	// attacker's hostname.
	for _, path := range []string{"/", "/api/sync/status"} {
		req := newGuardRequest("GET", path, "")
		req.Host = "evil.example:43210"
		req.Header.Set(tokenHeader, "test-token")
		if rec := serve(ws, req); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s with foreign host: got %d, want 403", path, rec.Code)
		}
	}
}

func TestGuardServesIndexWithoutToken(t *testing.T) {
	ws := guardedServer(t)

	for _, host := range []string{testHost, "localhost:43210"} {
		req := newGuardRequest("GET", "/", "")
		req.Host = host
		if rec := serve(ws, req); rec.Code != http.StatusOK {
			t.Errorf("GET / on %s: got %d, want 200", host, rec.Code)
		}
	}
}

func TestSessionTokenUsesConfiguredToken(t *testing.T) {
	cfg := &config.Config{}
	cfg.Web.Token = "  my-bookmark-token  "

	if got := sessionToken(cfg); got != "my-bookmark-token" {
		t.Errorf("sessionToken = %q, want configured token", got)
	}
}

func TestSessionTokenRandomByDefault(t *testing.T) {
	cfg := &config.Config{}

	first, second := sessionToken(cfg), sessionToken(cfg)
	if first == "" || first == second {
		t.Errorf("expected distinct non-empty random tokens, got %q and %q", first, second)
	}
}
