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

func TestMissingAssetDoesNotReturnHTMLFallback(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), false)
	req := httptest.NewRequest(http.MethodGet, "http://web.oboard.fun/js.js", nil)
	req.Host = "web.oboard.fun"
	req.Header.Set("Accept", "*/*")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected missing asset to return 404, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatal("missing asset returned HTML fallback")
	}
}

func TestMissingRouteStillReturnsHTMLFallback(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), false)
	req := httptest.NewRequest(http.MethodGet, "http://web.oboard.fun/dashboard", nil)
	req.Host = "web.oboard.fun"
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected route fallback to return 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TurboMesh") {
		t.Fatal("expected HTML fallback content")
	}
}

func TestTLSAskAllowsBaseAndValidSlugOnly(t *testing.T) {
	handler := NewHTTPHandler("web.oboard.fun.", "missing-dist", NewSignalHub(), false)

	for _, domain := range []string{"web.oboard.fun", "abc12345.web.oboard.fun"} {
		req := httptest.NewRequest(http.MethodGet, "/api/tls-ask?domain="+domain, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s should be allowed, got %d", domain, rec.Code)
		}
	}

	for _, domain := range []string{"evil.example.com", "bad.slug.web.oboard.fun", "-abc12345.web.oboard.fun"} {
		req := httptest.NewRequest(http.MethodGet, "/api/tls-ask?domain="+domain, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s should be forbidden, got %d", domain, rec.Code)
		}
	}
}
