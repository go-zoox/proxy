package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-zoox/proxy/utils/ascii"
)

// Proxy is a Powerful HTTP Proxy, inspired by Go Reverse Proxy.
type Proxy struct {
	onRequest  func(req *http.Request) error
	onResponse func(res *http.Response) error
	onError    func(err error, rw http.ResponseWriter, req *http.Request)

	bufferPool   BufferPool
	isAnonymouse bool
}

// Config is the configuration for the Proxy.
type Config struct {
	// IsAnonymouse is a flag to indicate whether the proxy is anonymouse.
	//	which means the proxy will not add headers:
	//		X-Forwarded-For
	//		X-Forwarded-Proto
	//		X-Forwarded-Host
	//		X-Forwarded-Port
	// Default is false.
	IsAnonymouse bool

	// OnRequest is a function that will be called before the request is sent.
	OnRequest func(req *http.Request) error

	// OnResponse is a function that will be called after the response is received.
	OnResponse func(res *http.Response) error

	// OnError is a function that will be called when an error occurs.
	OnError func(err error, rw http.ResponseWriter, req *http.Request)
}

// New creates a new Proxy.
func New(cfg *Config) *Proxy {
	onError := cfg.OnError
	if onError == nil {
		onError = defaultOnError
	}

	return &Proxy{
		onRequest:    cfg.OnRequest,
		onResponse:   cfg.OnResponse,
		onError:      onError,
		isAnonymouse: cfg.IsAnonymouse,
	}
}

// ServeHTTP is the entry point for the proxy.
func (r *Proxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	reqContext := req.Context()

	if cn, ok := rw.(http.CloseNotifier); ok {
		var cancel context.CancelFunc
		reqContext, cancel = context.WithCancel(reqContext)
		defer cancel()

		notify := cn.CloseNotify()
		go func() {
			select {
			case <-notify:
				cancel()
			case <-reqContext.Done():
			}
		}()
	}

	// create request by origin request
	request, err := r.createRequest(reqContext, rw, req)
	if err != nil {
		r.onError(err, rw, req)
		return
	}
	if request.Body != nil {
		// Reading from the request body after returning from a handler is not
		// allowed, and the RoundTrip goroutine that reads the Body can outlive
		// this handler. This can lead to a crash if the handler panics (see
		// Issue 46866). Although calling Close doesn't guarantee there isn't
		// any Read in flight after the handle returns, in practice it's safe to
		// read after closing it.
		defer request.Body.Close()
	}

	// create response by execute request
	response, err := r.createResponse(rw, request)
	if err != nil {
		r.onError(err, rw, request)
		return
	}

	// Deal with 101 Switchoing Protocols response: WebSocket, h2c, etc
	if response.StatusCode == http.StatusSwitchingProtocols {
		if !r.modifyResponse(rw, response, request) {
			return
		}

		r.handleUpgrade(rw, request, response)
		return
	}

	// http default
	// headers
	//	1. clean
	cleanResponseHeaders(response.Header)

	// modify response
	if !r.modifyResponse(rw, response, request) {
		return
	}

	//  2. copy
	copyHeaders(rw.Header(), response.Header)
	//  3. trailer
	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	announcedTrailers := len(response.Trailer)
	if announcedTrailers > 0 {
		trailerKeys := make([]string, 0, len(response.Trailer))
		for k := range response.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		rw.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
	}

	// status
	rw.WriteHeader(response.StatusCode)

	// copy buffer
	if err := r.copyResponse(rw, response.Body, r.flushInterval(response)); err != nil {
		defer response.Body.Close()

		// Since we're streaming the response, if we run into an error all we can do
		// is abort the request. Issue 23643: Proxy should use ErrAbortHandler
		// on read error while copying body.
		// if !shouldPanicOnCopyError(req) {
		if !shouldPanicOnCopyError(request) {
			log.Printf("suppressing panic for copyResponse error in test; copy error: %v", err)
			return
		}

		panic(http.ErrAbortHandler)
	}

	response.Body.Close() // close now, instead of defer, to populate res.Trailer
	if len(response.Trailer) > 0 {
		// Force chunking if we saw a response trailer
		// This prevents net/http from calculating the length for short
		// bodies and adding a Content-Length
		if fl, ok := rw.(http.Flusher); ok {
			fl.Flush()
		}
	}

	updateResponseTrailerHeaders(rw, response, announcedTrailers)
}

func (r *Proxy) modifyResponse(rw http.ResponseWriter, res *http.Response, req *http.Request) bool {
	if r.onResponse == nil {
		return true
	}

	if err := r.onResponse(res); err != nil {
		res.Body.Close()
		r.onError(err, rw, req)
		return false
	}

	return true
}

func (r *Proxy) copyResponse(dst io.Writer, src io.Reader, flushInterval time.Duration) error {
	if flushInterval != 0 {
		if wf, ok := dst.(writeFlusher); ok {
			mlw := &maxLatencyWriter{
				dst:     wf,
				latency: flushInterval,
			}
			defer mlw.stop()

			// set up initial timer so headers get flushed even if body writes are delayed
			mlw.flushPending = true
			mlw.t = time.AfterFunc(flushInterval, mlw.delayedFlush)

			dst = mlw
		}
	}

	var buf []byte
	if r.bufferPool != nil {
		buf = r.bufferPool.Get()
		defer r.bufferPool.Put(buf)
	}
	_, err := copyBuffer(dst, src, buf)
	return err
}

func (r *Proxy) flushInterval(res *http.Response) time.Duration {
	resCT := res.Header.Get("content-type")

	// For Server-Sent Events response, flush immediately
	// The MIME type is defined in https://www.w3.org/TR/eventsource/#text-event-stream
	if baseCT, _, _ := mime.ParseMediaType(resCT); baseCT == "text/event-stream" {
		return -1 // negative means immediately
	}

	// We might have the case of streaming for which Content-Length might be unset.
	if res.ContentLength == -1 {
		return -1
	}

	// return r.FlushInterval
	return 0
}

func (r *Proxy) handleUpgrade(rw http.ResponseWriter, req *http.Request, res *http.Response) {
	reqUpType := upgradeType(req.Header)
	resUpType := upgradeType(res.Header)
	if !ascii.IsPrint(resUpType) { // We know reqUpType is ASCII, it's checked by the caller.
		r.onError(fmt.Errorf("backend tried to switch to invalid protocol %q", resUpType), rw, req)
	}
	if !ascii.EqualFold(reqUpType, resUpType) {
		r.onError(fmt.Errorf("backend tried to switch protocol %q when %q was requested", resUpType, reqUpType), rw, req)
		return
	}

	hj, ok := rw.(http.Hijacker)
	if !ok {
		r.onError(fmt.Errorf("can't switch protocols using non-Hijacker ResponseWriter type %T", rw), rw, req)
		return
	}
	backConn, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		r.onError(fmt.Errorf("internal error: 101 switching protocols response with non-writable body"), rw, req)
		return
	}

	backConnCloseCh := make(chan bool)
	go func() {
		// Ensure that the cancellation of a request closes the backend.
		// See issue https://golang.org/issue/35559.
		select {
		case <-req.Context().Done():
		case <-backConnCloseCh:
		}
		backConn.Close()
	}()

	defer close(backConnCloseCh)

	conn, brw, err := hj.Hijack()
	if err != nil {
		r.onError(fmt.Errorf("hijack failed on protocol switch: %v", err), rw, req)
		return
	}
	defer conn.Close()

	copyHeaders(rw.Header(), res.Header)

	res.Header = rw.Header()
	res.Body = nil // so res.Write only writes the headers; we have res.Body in backConn above
	if err := res.Write(brw); err != nil {
		r.onError(fmt.Errorf("response write: %v", err), rw, req)
		return
	}
	if err := brw.Flush(); err != nil {
		r.onError(fmt.Errorf("response flush: %v", err), rw, req)
		return
	}
	errc := make(chan error, 1)
	spc := switchProtocolCopier{user: conn, backend: backConn}
	go spc.copyToBackend(errc)
	go spc.copyFromBackend(errc)
	<-errc
}
