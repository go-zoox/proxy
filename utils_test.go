package proxy

import (
	"net/http"
	"reflect"
	"testing"
)

func TestRemoveConnectionHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("Connection", "keep-alive, X-Test-1, , X-Test-2")
	h.Add("Connection", "Upgrade")
	h.Set("X-Test-1", "1")
	h.Set("X-Test-2", "2")
	h.Set("Upgrade", "websocket")
	h.Add("Connection", "keep-alive")

	removeConnectionHeaders(h)

	if got := h.Get("X-Test-1"); got != "" {
		t.Fatalf("expected X-Test-1 removed, got %q", got)
	}
	if got := h.Get("X-Test-2"); got != "" {
		t.Fatalf("expected X-Test-2 removed, got %q", got)
	}
	if got := h.Get("Upgrade"); got != "" {
		t.Fatalf("expected Upgrade removed, got %q", got)
	}
}

func TestParseHostPort(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantHost string
		wantPort string
	}{
		{name: "host only", input: "example.com", wantHost: "example.com", wantPort: "80"},
		{name: "host and port", input: "example.com:8080", wantHost: "example.com", wantPort: "8080"},
		{name: "empty port", input: "example.com:", wantHost: "example.com", wantPort: "80"},
		// keep compatibility with previous split-based behavior
		{name: "multi colon keeps first port segment", input: "a:b:c", wantHost: "a", wantPort: "b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotPort := ParseHostPort(tt.input)
			if !reflect.DeepEqual([]string{gotHost, gotPort}, []string{tt.wantHost, tt.wantPort}) {
				t.Fatalf("ParseHostPort(%q) = (%q,%q), want (%q,%q)", tt.input, gotHost, gotPort, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestShouldLogDefaultOnError(t *testing.T) {
	defaultOnErrorCounter = 0
	if !shouldLogDefaultOnError(1) {
		t.Fatalf("sampleEvery=1 should always log")
	}

	defaultOnErrorCounter = 0
	got := []bool{
		shouldLogDefaultOnError(3),
		shouldLogDefaultOnError(3),
		shouldLogDefaultOnError(3),
		shouldLogDefaultOnError(3),
		shouldLogDefaultOnError(3),
		shouldLogDefaultOnError(3),
	}
	want := []bool{false, false, true, false, false, true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sampleEvery=3 sequence mismatch: got %v, want %v", got, want)
	}
}

func BenchmarkRemoveConnectionHeaders(b *testing.B) {
	base := http.Header{}
	base.Add("Connection", "keep-alive, X-Test-1, X-Test-2, Upgrade, X-Test-3")
	base.Set("X-Test-1", "1")
	base.Set("X-Test-2", "2")
	base.Set("X-Test-3", "3")
	base.Set("Upgrade", "websocket")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := base.Clone()
		removeConnectionHeaders(h)
	}
}

func BenchmarkParseHostPort(b *testing.B) {
	input := "example.com:8080"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseHostPort(input)
	}
}
