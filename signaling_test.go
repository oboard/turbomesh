package main

import "testing"

func TestAllowedSignalTypesExcludePayloadFrames(t *testing.T) {
	for _, typ := range []string{"answer", "ice", "error"} {
		if !allowedClientSignal(typ) {
			t.Fatalf("client signal %s should be allowed", typ)
		}
	}
	for _, typ := range []string{"offer", "ice", "error"} {
		if !allowedBrowserSignal(typ) {
			t.Fatalf("browser signal %s should be allowed", typ)
		}
	}
	for _, typ := range []string{"http-request", "http-response", "ws-send", "payload"} {
		if allowedClientSignal(typ) || allowedBrowserSignal(typ) {
			t.Fatalf("application payload type %s must not be allowed through signaling", typ)
		}
	}
}

func TestValidateSlug(t *testing.T) {
	valid := []string{"abc12345", "session-123", "a2345678"}
	for _, slug := range valid {
		if err := validateSlug(slug); err != nil {
			t.Fatalf("%s should be valid: %v", slug, err)
		}
	}

	invalid := []string{"short", "-abc12345", "abc12345-", "ABC12345", "has.dot"}
	for _, slug := range invalid {
		if err := validateSlug(slug); err == nil {
			t.Fatalf("%s should be invalid", slug)
		}
	}
}
