package proxy

import (
	"bufio"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-zoox/proxy/utils/rewriter"
	"github.com/tidwall/gjson"
)

func TestSingleTarget(t *testing.T) {
	p := NewSingleHost("https://httpbin.zcorky.com", &SingleHostConfig{
		// Scheme: "https",
		Query: url.Values{
			"foo": []string{"bar"},
		},
		RequestHeaders: http.Header{
			// "Host":            []string{"httpbin.zcorky.com"},
			"x-custom-header": []string{"custom"},
		},
		ResponseHeaders: http.Header{
			"x-custom-response": []string{"custom"},
		},
	})

	req := httptest.NewRequest("GET", "/get?foo2=bar2", nil)
	req.Host = "httpbin.zcorky.com"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	res := w.Result()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", res.StatusCode)
	}
	if res.Header.Get("x-custom-response") != "custom" {
		t.Errorf("Expected custom, got %s", res.Header.Get("x-custom-response"))
	}

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	json := gjson.Parse(string(data))

	if json.Get("headers.host").String() != "httpbin.zcorky.com" {
		t.Errorf("Expected headers.host to be httpbin.zcorky.com, got %s", json.Get("headers.host").String())
	}

	if json.Get("headers.x-custom-header").String() != "custom" {
		t.Errorf("Expected headers.x-custom-header to be custom, got %s", json.Get("headers.x-custom-header").String())
	}

	if json.Get("headers.x-real-ip").String() == "" {
		t.Errorf("Expected headers.x-real-ip to be set, got empty")
	}

	if json.Get("headers.x-forwarded-for").String() == "" {
		t.Errorf("Expected headers.x-forwarded-for to be set, got empty")
	}

	if json.Get("headers.x-forwarded-proto").String() == "" {
		t.Errorf("Expected headers.x-forwarded-proto to be set, got empty")
	}

	if json.Get("headers.x-forwarded-host").String() == "" {
		t.Errorf("Expected headers.x-forwarded-host to be set, got empty")
	}

	if json.Get("headers.x-forwarded-port").String() == "" {
		t.Errorf("Expected headers.x-forwarded-port to be set, got empty")
	}

	if json.Get("query.foo").String() != "bar" {
		t.Errorf("Expected query.foo to be bar, got %s", json.Get("query.foo").String())
	}

	if json.Get("query.foo2").String() != "bar2" {
		t.Errorf("Expected query.foo2 to be bar2, got %s", json.Get("query.foo2").String())
	}
}

func TestSingleTargetRewrites(t *testing.T) {
	p := NewSingleHost("https://httpbin.zcorky.com", &SingleHostConfig{
		//
		// Rewrites: map[string]string{
		// 	// "/api/v1/uuid": "/uuid",
		// 	"/api/v1/(.*)": "/$1",
		// },
		Rewrites: rewriter.Rewriters{
			{From: "/api/v1/(.*)", To: "/$1"},
		},
	})

	req := httptest.NewRequest("GET", "/api/v1/uuid", nil)
	req.Host = "httpbin.zcorky.com"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	res := w.Result()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", res.StatusCode)
	}

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	json := gjson.Parse(string(data))

	if json.Get("uuid").String() == "" {
		t.Errorf("Expected uuid to be set, got empty")
	}
}

// Issue 16875: remove any proxied headers mentioned in the "Connection"
// header value.
func TestReverseProxyStripHeadersPresentInConnection(t *testing.T) {
	const fakeConnectionToken = "X-Fake-Connection-Token"
	const backendResponse = "I am the backend"

	// someConnHeader is some arbitrary header to be declared as a hop-by-hop header
	// in the Request's Connection header.
	const someConnHeader = "X-Some-Conn-Header"

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := r.Header.Get("Connection"); c != "" {
			t.Errorf("handler got header %q = %q; want empty", "Connection", c)
		}
		if c := r.Header.Get(fakeConnectionToken); c != "" {
			t.Errorf("handler got header %q = %q; want empty", fakeConnectionToken, c)
		}
		if c := r.Header.Get(someConnHeader); c != "" {
			t.Errorf("handler got header %q = %q; want empty", someConnHeader, c)
		}
		w.Header().Add("Connection", "Upgrade, "+fakeConnectionToken)
		w.Header().Add("Connection", someConnHeader)
		w.Header().Set(someConnHeader, "should be deleted")
		w.Header().Set(fakeConnectionToken, "should be deleted")
		io.WriteString(w, backendResponse)
	}))
	defer backend.Close()
	// backendURL, err := url.Parse(backend.URL)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	proxyHandler := NewSingleHost(backend.URL)
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler.ServeHTTP(w, r)
		if c := r.Header.Get(someConnHeader); c != "should be deleted" {
			t.Errorf("handler modified header %q = %q; want %q", someConnHeader, c, "should be deleted")
		}
		if c := r.Header.Get(fakeConnectionToken); c != "should be deleted" {
			t.Errorf("handler modified header %q = %q; want %q", fakeConnectionToken, c, "should be deleted")
		}
		c := r.Header["Connection"]
		var cf []string
		for _, f := range c {
			for _, sf := range strings.Split(f, ",") {
				if sf = strings.TrimSpace(sf); sf != "" {
					cf = append(cf, sf)
				}
			}
		}
		sort.Strings(cf)
		expectedValues := []string{"Upgrade", someConnHeader, fakeConnectionToken}
		sort.Strings(expectedValues)
		if !reflect.DeepEqual(cf, expectedValues) {
			t.Errorf("handler modified header %q = %q; want %q", "Connection", cf, expectedValues)
		}
	}))
	defer frontend.Close()

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	getReq.Header.Add("Connection", "Upgrade, "+fakeConnectionToken)
	getReq.Header.Add("Connection", someConnHeader)
	getReq.Header.Set(someConnHeader, "should be deleted")
	getReq.Header.Set(fakeConnectionToken, "should be deleted")
	res, err := frontend.Client().Do(getReq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if got, want := string(bodyBytes), backendResponse; got != want {
		t.Errorf("got body %q; want %q", got, want)
	}
	if c := res.Header.Get("Connection"); c != "" {
		t.Errorf("handler got header %q = %q; want empty", "Connection", c)
	}
	if c := res.Header.Get(someConnHeader); c != "" {
		t.Errorf("handler got header %q = %q; want empty", someConnHeader, c)
	}
	if c := res.Header.Get(fakeConnectionToken); c != "" {
		t.Errorf("handler got header %q = %q; want empty", fakeConnectionToken, c)
	}
}

func TestReverseProxyStripEmptyConnection(t *testing.T) {
	// See Issue 46313.
	const backendResponse = "I am the backend"

	// someConnHeader is some arbitrary header to be declared as a hop-by-hop header
	// in the Request's Connection header.
	const someConnHeader = "X-Some-Conn-Header"

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := r.Header.Values("Connection"); len(c) != 0 {
			t.Errorf("handler got header %q = %v; want empty", "Connection", c)
		}
		if c := r.Header.Get(someConnHeader); c != "" {
			t.Errorf("handler got header %q = %q; want empty", someConnHeader, c)
		}
		w.Header().Add("Connection", "")
		w.Header().Add("Connection", someConnHeader)
		w.Header().Set(someConnHeader, "should be deleted")
		io.WriteString(w, backendResponse)
	}))
	defer backend.Close()
	// backendURL, err := url.Parse(backend.URL)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	proxyHandler := NewSingleHost(backend.URL)
	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHandler.ServeHTTP(w, r)
		if c := r.Header.Get(someConnHeader); c != "should be deleted" {
			t.Errorf("handler modified header %q = %q; want %q", someConnHeader, c, "should be deleted")
		}
	}))
	defer frontend.Close()

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	getReq.Header.Add("Connection", "")
	getReq.Header.Add("Connection", someConnHeader)
	getReq.Header.Set(someConnHeader, "should be deleted")
	res, err := frontend.Client().Do(getReq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if got, want := string(bodyBytes), backendResponse; got != want {
		t.Errorf("got body %q; want %q", got, want)
	}
	if c := res.Header.Get("Connection"); c != "" {
		t.Errorf("handler got header %q = %q; want empty", "Connection", c)
	}
	if c := res.Header.Get(someConnHeader); c != "" {
		t.Errorf("handler got header %q = %q; want empty", someConnHeader, c)
	}
}

func TestXForwardedFor(t *testing.T) {
	const prevForwardedFor = "client ip"
	const backendResponse = "I am the backend"
	const backendStatus = 404
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") == "" {
			t.Errorf("didn't get X-Forwarded-For header")
		}
		if !strings.Contains(r.Header.Get("X-Forwarded-For"), prevForwardedFor) {
			t.Errorf("X-Forwarded-For didn't contain prior data")
		}
		w.WriteHeader(backendStatus)
		w.Write([]byte(backendResponse))
	}))
	defer backend.Close()
	// backendURL, err := url.Parse(backend.URL)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	proxyHandler := NewSingleHost(backend.URL)
	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	getReq.Header.Set("Connection", "close")
	getReq.Header.Set("X-Forwarded-For", prevForwardedFor)
	getReq.Close = true
	res, err := frontend.Client().Do(getReq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if g, e := res.StatusCode, backendStatus; g != e {
		t.Errorf("got res.StatusCode %d; expected %d", g, e)
	}
	bodyBytes, _ := io.ReadAll(res.Body)
	if g, e := string(bodyBytes), backendResponse; g != e {
		t.Errorf("got body %q; expected %q", g, e)
	}
}

// Issue 38079: don't append to X-Forwarded-For if it's present but nil
func TestXForwardedFor_Omit(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			t.Errorf("got X-Forwarded-For header: %q", v)
		}
		w.Write([]byte("hi"))
	}))
	defer backend.Close()
	// backendURL, err := url.Parse(backend.URL)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	proxyHandler := NewSingleHost(backend.URL)
	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()

	oldOnRequest := proxyHandler.OnRequest
	proxyHandler.OnRequest = func(req, originReq *http.Request) error {
		req.Header["X-Forwarded-For"] = nil
		return oldOnRequest(req, originReq)
	}

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	getReq.Host = "some-name"
	getReq.Close = true
	res, err := frontend.Client().Do(getReq)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	res.Body.Close()
}

// func TestReverseProxyRewriteStripsForwarded(t *testing.T) {
// 	headers := []string{
// 		"Forwarded",
// 		"X-Forwarded-For",
// 		"X-Forwarded-Host",
// 		"X-Forwarded-Proto",
// 	}
// 	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		for _, h := range headers {
// 			if v := r.Header.Get(h); v != "" {
// 				t.Errorf("got %v header: %q", h, v)
// 			}
// 		}
// 	}))
// 	defer backend.Close()
// 	backendURL, err := url.Parse(backend.URL)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	proxyHandler := &Proxy{
// 		// Rewrite: func(r *ProxyRequest) {
// 		// 	r.SetURL(backendURL)
// 		// },
// 		OnRequest: func(req, originReq *http.Request) error {
// 			req.URL.Scheme = backendURL.Scheme
// 			req.URL.Host = backendURL.Host
// 			req.URL.Path = backendURL.Path
// 			req.Host = ""
// 			return nil
// 		},
// 	}
// 	frontend := httptest.NewServer(proxyHandler)
// 	defer frontend.Close()

// 	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
// 	getReq.Host = "some-name"
// 	getReq.Close = true
// 	for _, h := range headers {
// 		getReq.Header.Set(h, "x")
// 	}
// 	res, err := frontend.Client().Do(getReq)
// 	if err != nil {
// 		t.Fatalf("Get: %v", err)
// 	}
// 	res.Body.Close()
// }

var proxyQueryTests = []struct {
	baseSuffix string // suffix to add to backend URL
	reqSuffix  string // suffix to add to frontend's request URL
	want       string // what backend should see for final request URL (without ?)
}{
	{"", "", ""},
	{"?sta=tic", "?us=er", "sta=tic&us=er"},
	{"", "?us=er", "us=er"},
	{"?sta=tic", "", "sta=tic"},
}

// func TestReverseProxyQuery(t *testing.T) {
// 	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.Header().Set("X-Got-Query", r.URL.RawQuery)
// 		w.Write([]byte("hi"))
// 	}))
// 	defer backend.Close()

// 	for i, tt := range proxyQueryTests {
// 		// backendURL, err := url.Parse(backend.URL + tt.baseSuffix)
// 		// if err != nil {
// 		// 	t.Fatal(err)
// 		// }
// 		frontend := httptest.NewServer(NewSingleHost(backend.URL + tt.baseSuffix))
// 		req, _ := http.NewRequest("GET", frontend.URL+tt.reqSuffix, nil)
// 		req.Close = true
// 		res, err := frontend.Client().Do(req)
// 		if err != nil {
// 			t.Fatalf("%d. Get: %v", i, err)
// 		}
// 		if g, e := res.Header.Get("X-Got-Query"), tt.want; g != e {
// 			t.Errorf("%d. got query %q; expected %q", i, g, e)
// 		}
// 		res.Body.Close()
// 		frontend.Close()
// 	}
// }

// func TestReverseProxyFlushInterval(t *testing.T) {
// 	const expected = "hi"
// 	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.Write([]byte(expected))
// 	}))
// 	defer backend.Close()

// 	// backendURL, err := url.Parse(backend.URL)
// 	// if err != nil {
// 	// 	t.Fatal(err)
// 	// }

// 	proxyHandler := NewSingleHost(backend.URL)
// 	proxyHandler.FlushInterval = time.Microsecond

// 	frontend := httptest.NewServer(proxyHandler)
// 	defer frontend.Close()

// 	req, _ := http.NewRequest("GET", frontend.URL, nil)
// 	req.Close = true
// 	res, err := frontend.Client().Do(req)
// 	if err != nil {
// 		t.Fatalf("Get: %v", err)
// 	}
// 	defer res.Body.Close()
// 	if bodyBytes, _ := io.ReadAll(res.Body); string(bodyBytes) != expected {
// 		t.Errorf("got body %q; expected %q", bodyBytes, expected)
// 	}
// }

type mockFlusher struct {
	http.ResponseWriter
	flushed bool
}

func (m *mockFlusher) Flush() {
	m.flushed = true
}

type wrappedRW struct {
	http.ResponseWriter
}

func (w *wrappedRW) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// func TestReverseProxyResponseControllerFlushInterval(t *testing.T) {
// 	const expected = "hi"
// 	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.Write([]byte(expected))
// 	}))
// 	defer backend.Close()

// 	backendURL, err := url.Parse(backend.URL)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	mf := &mockFlusher{}
// 	proxyHandler := NewSingleHost(backendURL)
// 	proxyHandler.FlushInterval = -1 // flush immediately
// 	proxyWithMiddleware := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		mf.ResponseWriter = w
// 		w = &wrappedRW{mf}
// 		proxyHandler.ServeHTTP(w, r)
// 	})

// 	frontend := httptest.NewServer(proxyWithMiddleware)
// 	defer frontend.Close()

// 	req, _ := http.NewRequest("GET", frontend.URL, nil)
// 	req.Close = true
// 	res, err := frontend.Client().Do(req)
// 	if err != nil {
// 		t.Fatalf("Get: %v", err)
// 	}
// 	defer res.Body.Close()
// 	if bodyBytes, _ := io.ReadAll(res.Body); string(bodyBytes) != expected {
// 		t.Errorf("got body %q; expected %q", bodyBytes, expected)
// 	}
// 	if !mf.flushed {
// 		t.Errorf("response writer was not flushed")
// 	}
// }

// func TestReverseProxyFlushIntervalHeaders(t *testing.T) {
// 	const expected = "hi"
// 	stopCh := make(chan struct{})
// 	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		w.Header().Add("MyHeader", expected)
// 		w.WriteHeader(200)
// 		w.(http.Flusher).Flush()
// 		<-stopCh
// 	}))
// 	defer backend.Close()
// 	defer close(stopCh)

// 	// backendURL, err := url.Parse(backend.URL)
// 	// if err != nil {
// 	// 	t.Fatal(err)
// 	// }

// 	proxyHandler := NewSingleHost(backend.URL)
// 	proxyHandler.FlushInterval = time.Microsecond

// 	frontend := httptest.NewServer(proxyHandler)
// 	defer frontend.Close()

// 	req, _ := http.NewRequest("GET", frontend.URL, nil)
// 	req.Close = true

// 	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
// 	defer cancel()
// 	req = req.WithContext(ctx)

// 	res, err := frontend.Client().Do(req)
// 	if err != nil {
// 		t.Fatalf("Get: %v", err)
// 	}
// 	defer res.Body.Close()

// 	if res.Header.Get("MyHeader") != expected {
// 		t.Errorf("got header %q; expected %q", res.Header.Get("MyHeader"), expected)
// 	}
// }

func TestReverseProxyCancellation(t *testing.T) {
	const backendResponse = "I am the backend"

	reqInFlight := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(reqInFlight) // cause the client to cancel its request

		select {
		case <-time.After(10 * time.Second):
			// Note: this should only happen in broken implementations, and the
			// closenotify case should be instantaneous.
			t.Error("Handler never saw CloseNotify")
			return
		case <-w.(http.CloseNotifier).CloseNotify():
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(backendResponse))
	}))

	defer backend.Close()

	backend.Config.ErrorLog = log.New(io.Discard, "", 0)

	// backendURL, err := url.Parse(backend.URL)
	// if err != nil {
	// 	t.Fatal(err)
	// }

	proxyHandler := NewSingleHost(backend.URL)

	// Discards errors of the form:
	// http: proxy error: read tcp 127.0.0.1:44643: use of closed network connection
	// proxyHandler.ErrorLog = log.New(io.Discard, "", 0)

	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()
	frontendClient := frontend.Client()

	getReq, _ := http.NewRequest("GET", frontend.URL, nil)
	go func() {
		<-reqInFlight
		frontendClient.Transport.(*http.Transport).CancelRequest(getReq)
	}()
	res, err := frontendClient.Do(getReq)
	if res != nil {
		t.Errorf("got response %v; want nil", res.Status)
	}
	if err == nil {
		// This should be an error like:
		// Get "http://127.0.0.1:58079": read tcp 127.0.0.1:58079:
		//    use of closed network connection
		t.Error("Server.Client().Do() returned nil error; want non-nil error")
	}
}

func req(t *testing.T, v string) *http.Request {
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(v)))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// Issue 12344
func TestNilBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	}))
	defer backend.Close()

	frontend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// backURL, _ := url.Parse(backend.URL)
		rp := NewSingleHost(backend.URL)
		r := req(t, "GET / HTTP/1.0\r\n\r\n")
		r.Body = nil // this accidentally worked in Go 1.4 and below, so keep it working
		rp.ServeHTTP(w, r)
	}))
	defer frontend.Close()

	res, err := http.Get(frontend.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	slurp, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(slurp) != "hi" {
		t.Errorf("Got %q; want %q", slurp, "hi")
	}
}

// Issue 15524
func TestUserAgentHeader(t *testing.T) {
	var gotUA string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
	}))
	defer backend.Close()
	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}

	proxyHandler := New(&Config{
		OnRequest: func(outReq *http.Request, inReq *http.Request) error {
			outReq.URL = backendURL
			return nil
		},
	}) // new(ReverseProxy)
	// proxyHandler.ErrorLog = log.New(io.Discard, "", 0) // quiet for tests
	// proxyHandler.Director = func(req *http.Request) {
	// 	req.URL = backendURL
	// }
	frontend := httptest.NewServer(proxyHandler)
	defer frontend.Close()
	frontendClient := frontend.Client()

	for _, sentUA := range []string{"explicit UA", ""} {
		getReq, _ := http.NewRequest("GET", frontend.URL, nil)
		getReq.Header.Set("User-Agent", sentUA)
		getReq.Close = true
		res, err := frontendClient.Do(getReq)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		res.Body.Close()
		if got, want := gotUA, sentUA; got != want {
			t.Errorf("got forwarded User-Agent %q, want %q", got, want)
		}
	}
}
