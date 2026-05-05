package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHomepageRedirectsToHTTPSWhenForwardedProtoHTTP(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), true)
	req := httptest.NewRequest(http.MethodGet, "http://web.oboard.fun/", nil)
	req.Host = "web.oboard.fun"
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "https://web.oboard.fun/" {
		t.Fatalf("unexpected Location %q", got)
	}
}

func TestWildcardDoesNotForceHTTPSRedirect(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), true)
	req := httptest.NewRequest(http.MethodGet, "http://abc123.web.oboard.fun/", nil)
	req.Host = "abc123.web.oboard.fun"
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusTemporaryRedirect {
		t.Fatal("wildcard service must not be force-upgraded to HTTPS")
	}
}

func TestHomepageServesFallbackWhenStaticMissing(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), false)
	req := httptest.NewRequest(http.MethodGet, "http://web.oboard.fun/", nil)
	req.Host = "web.oboard.fun"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TurboMesh") {
		t.Fatal("expected fallback homepage content")
	}
}
