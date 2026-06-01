package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

const fakeHopHeader = "X-Fake-Hop-Header-For-Test"

func init() {
	inOurTests = true
	hopHeaders = append(hopHeaders, fakeHopHeader)
}

func TestProxy(t *testing.T) {
	const backendResponse = "I am the backend"
	const backendStatus = 404
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.FormValue("mode") == "hangup" {
			c, _, _ := w.(http.Hijacker).Hijack()
			c.Close()
			return
		}
		if len(r.TransferEncoding) > 0 {
			t.Errorf("backend got unexpected TransferEncoding: %v", r.TransferEncoding)
		}
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Errorf("didn't get X-Forwarded-For header")
		}
		if c := r.Header.Get("Connection"); c != "" {
			t.Errorf("handler got Connection header value %q", c)
		}
		if c := r.Header.Get("Te"); c != "trailers" {
			t.Errorf("handler got Te header value %q; want 'trailers'", c)
		}
		if c := r.Header.Get("Upgrade"); c != "" {
			t.Errorf("handler got Upgrade header value %q", c)
		}
		if c := r.Header.Get("Proxy-Connection"); c != "" {
			t.Errorf("handler got Proxy-Connection header value %q", c)
		}
		// fmt.Printf("%#v", r.Header)
		// if c := r.Header.Get("Host"); c != "custom-host-header" {
		// 	t.Errorf("handler got Host header value %q", c)
		// }
		if g, e := r.Host, "some-name"; g != e {
			t.Errorf("backend got Host header %q, want %q", g, e)
		}
		w.Header().Set("Trailers", "not a special header field name")
		w.Header().Set("Trailer", "X-Trailer")
		w.Header().Set("X-Foo", "bar")
		w.Header().Set("Upgrade", "foo")
		w.Header().Set(fakeHopHeader, "foo")
		w.Header().Add("X-Multi-Value", "foo")
		w.Header().Add("X-Multi-Value", "bar")
		http.SetCookie(w, &http.Cookie{Name: "flavor", Value: "chocolateChip"})
		w.WriteHeader(backendStatus)
		w.Write([]byte(backendResponse))
		w.Header().Set("X-Trailer", "trailer_value")
		w.Header().Set(http.TrailerPrefix+"X-Unannounced-Trailer", "unannounced_trailer_value")
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	// proxyHandler := httputil.NewSingleHostReverseProxy(backendURL)
	// proxyHandler.ErrorLog = log.New(io.Discard, "", 0) // quiet for tests

	proxyHandler := New(&Config{
		OnRequest: func(outReq, inReq *http.Request) error {
			outReq.URL.Scheme = backendURL.Scheme
			outReq.URL.Host = backendURL.Host

			return nil
		},
	})
	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()
	frontendClient := frontend.Client()

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	getReq.Host = "some-name"
	// getReq.Header.Set("Host", "custom-host-header")
	getReq.Header.Set("Connection", "close, TE")
	getReq.Header.Add("Te", "foo")
	getReq.Header.Add("Te", "bar, trailers")
	getReq.Header.Set("Proxy-Connection", "should be deleted")
	getReq.Header.Set("Upgrade", "foo")
	getReq.Close = true
	res, err := frontendClient.Do(getReq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if g, e := res.StatusCode, backendStatus; g != e {
		t.Errorf("got res.StatusCode %d; expected %d", g, e)
	}
	if g, e := res.Header.Get("X-Foo"), "bar"; g != e {
		t.Errorf("got X-Foo %q; expected %q", g, e)
	}
	if c := res.Header.Get(fakeHopHeader); c != "" {
		t.Errorf("got %s header value %q", fakeHopHeader, c)
	}
	if g, e := res.Header.Get("Trailers"), "not a special header field name"; g != e {
		t.Errorf("header Trailers = %q; want %q", g, e)
	}
	if g, e := len(res.Header["X-Multi-Value"]), 2; g != e {
		t.Errorf("got %d X-Multi-Value header values; expected %d", g, e)
	}
	if g, e := len(res.Header["Set-Cookie"]), 1; g != e {
		t.Fatalf("got %d SetCookies, want %d", g, e)
	}
	if g, e := res.Trailer, (http.Header{"X-Trailer": nil}); !reflect.DeepEqual(g, e) {
		t.Errorf("before reading body, Trailer = %#v; want %#v", g, e)
	}
	if cookie := res.Cookies()[0]; cookie.Name != "flavor" {
		t.Errorf("unexpected cookie %q", cookie.Name)
	}
	bodyBytes, _ := io.ReadAll(res.Body)
	if g, e := string(bodyBytes), backendResponse; g != e {
		t.Errorf("got body %q; expected %q", g, e)
	}
	if g, e := res.Trailer.Get("X-Trailer"), "trailer_value"; g != e {
		t.Errorf("Trailer(X-Trailer) = %q ; want %q", g, e)
	}
	if g, e := res.Trailer.Get("X-Unannounced-Trailer"), "unannounced_trailer_value"; g != e {
		t.Errorf("Trailer(X-Unannounced-Trailer) = %q ; want %q", g, e)
	}

	// Test that a backend failing to be reached or one which doesn't return
	// a response results in a StatusBadGateway.
	getReq, _ = http.NewRequest("GET", frontend.URL+"/?mode=hangup", nil)
	getReq.Close = true
	res, err = frontendClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Errorf("request to bad proxy = %v; want 502 StatusBadGateway", res.Status)
	}
}

func BenchmarkProxyCopyResponse_WithBufferPool(b *testing.B) {
	p := New(&Config{})
	body := bytes.Repeat([]byte("a"), 64*1024)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(body)
		if err := p.copyResponse(io.Discard, src, 0); err != nil {
			b.Fatalf("copy response: %v", err)
		}
	}
}

func BenchmarkProxyCopyResponse_WithoutBufferPool(b *testing.B) {
	p := New(&Config{})
	p.bufferPool = nil
	body := bytes.Repeat([]byte("a"), 64*1024)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		src := bytes.NewReader(body)
		if err := p.copyResponse(io.Discard, src, 0); err != nil {
			b.Fatalf("copy response: %v", err)
		}
	}
}

func TestProxyForwardedHeadersTrustProxyPriority(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-Forwarded-Proto"), "https"; got != want {
			t.Fatalf("X-Forwarded-Proto: got %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-Forwarded-Host"), "public.example.com"; got != want {
			t.Fatalf("X-Forwarded-Host: got %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-Forwarded-Port"), "443"; got != want {
			t.Fatalf("X-Forwarded-Port: got %q, want %q", got, want)
		}
		if got, want := r.Header.Get("X-Forwarded-Target"), "https://public.example.com"; got != want {
			t.Fatalf("X-Forwarded-Target: got %q, want %q", got, want)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxyHandler := New(&Config{
		TrustProxy: true,
		OnRequest: func(outReq, inReq *http.Request) error {
			outReq.URL.Scheme = backendURL.Scheme
			outReq.URL.Host = backendURL.Host
			return nil
		},
	})

	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()

	req, _ := http.NewRequest(http.MethodGet, frontend.URL, nil)
	req.Host = "inner.gateway.local:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "public.example.com")
	req.Header.Set("X-Forwarded-Port", "443")

	res, err := frontend.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	res.Body.Close()
}
