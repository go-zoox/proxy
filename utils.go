package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-zoox/compress/flate"
	"github.com/go-zoox/compress/gzip"
	"golang.org/x/net/http/httpguts"
)

func getUpgradeType(h *http.Header) string {
	if strings.ToLower(h.Get("connection")) == "upgrade" {
		return strings.ToLower(h.Get("upgrade"))
	}

	return ""
}

// Hop-by-hop headers. These are removed when sent to the backend.
// As of RFC 7230, hop-by-hop headers are required to appear in the
// Connection header field. These are the headers defined by the
// obsoleted RFC 2616 (section 13.5.1) and are used for backward
// compatibility.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above; https://www.rfc-editor.org/errata_search.php?eid=4522
	"Transfer-Encoding",
	"Upgrade",
	"Strict-Transport-Security", // HSTS
	"Date",
}

func cleanRequestHeaders(h http.Header) {
	// connection
	removeConnectionHeaders(h)

	// common
	removeCommonHeaders(h)

	// Issue 21096: tell backend applications that care about trailer support
	// that we support trailers. (We do, but we don't go out of our way to
	// advertise that unless the incoming client request thought it was worth
	// mentioning.) Note that we look at req.Header, not outreq.Header, since
	// the latter has passed through removeConnectionHeaders.
	if httpguts.HeaderValuesContainsToken(h["Te"], "trailers") {
		h.Set("Te", "trailers")
	}
}

func addRequestHeaders(h http.Header, req *http.Request, isAnonymouse bool) {
	// real ip
	h.Set("x-real-ip", req.RemoteAddr)

	// x-forwarded-XXXX
	host, port := ParseHostPort(req.Host)
	scheme := req.URL.Scheme
	if scheme == "" {
		scheme = "http"
	}

	// if not anonymouse, add headers:
	//   x-forwarded-proto
	//   x-forwarded-host
	//   x-forwarded-port
	if !isAnonymouse {
		h.Set(HeaderXForwardedProto, scheme)
		h.Set(HeaderXForwardedHost, host)
		h.Set(HeaderXForwardedPort, port)
	}
}

func updateRequestUpgradeHeaders(h http.Header, upgrade string) {
	// After stripping all the hop-by-hop connection headers above, add back any
	// necessary for protocol upgrades, such as for websockets
	if upgrade != "" {
		h.Set("Connection", "Upgrade")
		h.Set("Upgrade", upgrade)
	}
}

func updateRequestXForwardedForHeader(h http.Header, req *http.Request, isAnonymouse bool) {
	if isAnonymouse {
		return
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		prior, ok := req.Header[HeaderXForwardedFor]
		omit := ok && prior == nil // Issue 38079: nil now means don't populate the header
		if len(prior) > 0 {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		if !omit {
			h.Set(HeaderXForwardedFor, clientIP)
		}
	}
}

func cleanResponseHeaders(h http.Header) {
	// connection
	removeConnectionHeaders(h)

	// common
	removeCommonHeaders(h)
}

func updateResponseTrailerHeaders(rw http.ResponseWriter, response *http.Response, announcedTrailers int) {
	if len(response.Trailer) == announcedTrailers {
		copyHeaders(rw.Header(), response.Trailer)
		return
	}

	for k, vv := range response.Trailer {
		k = http.TrailerPrefix + k
		for _, v := range vv {
			rw.Header().Add(k, v)
		}
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func removeCommonHeaders(h http.Header) {
	for _, header := range hopHeaders {
		h.Del(header)
	}
}

func removeConnectionHeaders(h http.Header) {
	for _, f := range h["Connection"] {
		for _, sf := range strings.Split(f, ",") {
			if sf = textproto.TrimString(sf); sf != "" {
				h.Del(sf)
			}
		}
	}
}

func copyBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	if len(buf) == 0 {
		buf = make([]byte, 32*1024)
	}

	var written int64
	for {
		nr, rerr := src.Read(buf)
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if werr != nil {
				return written, werr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}

		if rerr != nil {
			if rerr == io.EOF {
				return written, nil
			}

			if rerr != context.Canceled {
				log.Printf("copyBuffer read from source error: %v", rerr)
			}

			return written, rerr
		}
	}
}

var inOurTests bool // whether we're in our own tests
func shouldPanicOnCopyError(req *http.Request) bool {
	if inOurTests {
		// Our tests know to handle this panic.
		return true
	}

	if req.Context().Value(http.ServerContextKey) != nil {
		// We seem to be running under an HTTP server, so
		// it'll recover the panic
		return true
	}

	// Otherwise act like Go 1.10 and earlier to not break
	// existing tests.
	return false
}

func defaultOnError(err error, rw http.ResponseWriter, req *http.Request) {
	status := http.StatusInternalServerError
	message := err.Error()

	// panic(err)

	if errX, ok := err.(*HTTPError); ok {
		if errX.Status() != 0 {
			status = errX.Status()
		}
	}

	log.Printf("error: %s (%s %s %d)\n", err, req.Method, req.URL.String(), status)

	// service unavailable: connection refused
	if strings.Contains(message, "connection refused") {
		status = http.StatusServiceUnavailable
		message = "Service Unavailable"
	}

	rw.WriteHeader(status)
	rw.Write([]byte(message))
}

// ParseHostPort parses host and port from a string in the form host[:port].
func ParseHostPort(rawHost string) (string, string) {
	arr := strings.Split(rawHost, ":")
	host := arr[0]
	port := ""
	if len(arr) > 1 {
		port = arr[1]
	}

	if port == "" {
		port = "80"
	}

	return host, port
}

// switchProtocolCopier exists so goroutines proxying data back and
// forth have nice names in stacks.
type switchProtocolCopier struct {
	user, backend io.ReadWriter
}

func (c switchProtocolCopier) copyFromBackend(errc chan<- error) {
	_, err := io.Copy(c.user, c.backend)
	errc <- err
}

func (c switchProtocolCopier) copyToBackend(errc chan<- error) {
	_, err := io.Copy(c.backend, c.user)
	errc <- err
}

type writeFlusher interface {
	io.Writer
	http.Flusher
}

type maxLatencyWriter struct {
	dst     writeFlusher
	latency time.Duration // non-zero; negative means to flush immediately

	mu           sync.Mutex // protects t, flushPending, and dst.Flush
	t            *time.Timer
	flushPending bool
}

func (m *maxLatencyWriter) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n, err = m.dst.Write(p)
	if m.latency < 0 {
		m.dst.Flush()
		return
	}

	if m.flushPending {
		return
	}

	if m.t == nil {
		m.t = time.AfterFunc(m.latency, m.delayedFlush)
	} else {
		m.t.Reset(m.latency)
	}

	m.flushPending = true
	return
}

func (m *maxLatencyWriter) delayedFlush() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.flushPending { // if stop was called but AfterFunc already started this goroutine
		return
	}

	m.dst.Flush()
	m.flushPending = false
}

func (m *maxLatencyWriter) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.flushPending = false
	if m.t != nil {
		m.t.Stop()
	}
}

func upgradeType(h http.Header) string {
	if strings.ToLower(h.Get("Connection")) != "upgrade" {
		return ""
	}

	return h.Get("upgrade")
}

func rewriteHTMLResponse(resp *http.Response, onRewrite func([]byte) ([]byte, error)) error {
	contentEncoding := resp.Header.Get("Content-Encoding")
	if contentEncoding == "" {
		//
	} else if contentEncoding == "gzip" {
		//
	} else if contentEncoding == "deflate" {
		//
	} else {
		fmt.Printf("unsupport content encoding: %s, ignore rewrite body\n", contentEncoding)
		return nil
	}

	b, err := ioutil.ReadAll(resp.Body) //Read html
	if err != nil {
		return err
	}
	err = resp.Body.Close()
	if err != nil {
		return err
	}

	if resp.Header.Get("Content-Encoding") == "" {
		// replace html
		// like nginx sub_filter
		// example: b = bytes.Replace(b, []byte("</body>"), []byte(`<div>custom</div></body>`), -1)
		b, err = onRewrite(b)
		if err != nil {
			return err
		}
	} else {
		if contentEncoding == "gzip" {
			g := gzip.New()
			if decodedB, err := g.Decompress(b); err != nil {
				return err
			} else {
				// replace html
				// like nginx sub_filter
				// example: b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>custom</div></body>`), -1) // replace html
				b, err = onRewrite(decodedB)
				if err != nil {
					return err
				}
				b = g.Compress(b)
			}
		} else if contentEncoding == "deflate" {
			d := flate.New()
			if decodedB, err := d.Decompress(b); err != nil {
				return err
			} else {
				// replace html
				// like nginx sub_filter
				// example: b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>custom</div></body>`), -1) // replace html
				b, err = onRewrite(decodedB)
				if err != nil {
					return err
				}

				b = d.Compress(b)
			}
		} else {
			return fmt.Errorf("unsupport content encoding: %s", contentEncoding)
		}
	}

	body := ioutil.NopCloser(bytes.NewReader(b))
	resp.Body = body
	resp.ContentLength = int64(len(b))
	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return nil
}
