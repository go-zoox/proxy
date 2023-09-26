package proxy

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-zoox/headers"
	"github.com/go-zoox/proxy/utils/ascii"
)

type key string

const stateKey key = "state"

// Proxy is a Powerful HTTP Proxy, inspired by Go Reverse Proxy.
type Proxy struct {
	OnContext func(ctx context.Context) (context.Context, error)
	//
	OnRequest  func(req, originReq *http.Request) error
	OnResponse func(res *http.Response, originReq *http.Request) error
	OnError    func(err error, rw http.ResponseWriter, req *http.Request)

	// IsAnonymouse is a flag to indicate whether the proxy is anonymouse.
	//	which means the proxy will not add headers:
	//		X-Forwarded-For
	//		X-Forwarded-Proto
	//		X-Forwarded-Host
	//		X-Forwarded-Port
	// Default is false.
	IsAnonymouse bool

	// Transport is the transport used to make requests to the Origin.
	Transport http.RoundTripper

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

	// OnContext is a function that will be called before the request is sent.
	OnContext func(ctx context.Context) (context.Context, error)

	// OnRequest is a function that will be called before the request is sent.
	OnRequest func(req, inReq *http.Request) error

	// OnResponse is a function that will be called after the response is received.
	OnResponse func(res *http.Response, inReq *http.Request) error

	// OnError is a function that will be called when an error occurs.
	OnError func(err error, rw http.ResponseWriter, req *http.Request)
}

// New creates a new Proxy.
func New(cfg *Config) *Proxy {
	p := &Proxy{
		OnContext:    cfg.OnContext,
		OnRequest:    cfg.OnRequest,
		OnResponse:   cfg.OnResponse,
		OnError:      cfg.OnError,
		isAnonymouse: cfg.IsAnonymouse,
	}

	if p.OnError == nil {
		p.OnError = defaultOnError
	}

	return p
}

// ServeHTTP is the entry point for the proxy.
func (r *Proxy) ServeHTTP(rw http.ResponseWriter, inReq *http.Request) {
	ctx := inReq.Context()

	if r.OnContext != nil {
		var err error
		ctx, err = r.OnContext(ctx)
		if err != nil {
			r.OnError(err, rw, inReq)
			return
		}
	}

	if cn, ok := rw.(http.CloseNotifier); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()

		notify := cn.CloseNotify()
		go func() {
			select {
			case <-notify:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	// create outReq by origin outReq
	outReq, err := r.createRequest(ctx, rw, inReq)
	if err != nil {
		r.OnError(err, rw, inReq)
		return
	}
	if outReq.Body != nil {
		// Reading from the request body after returning from a handler is not
		// allowed, and the RoundTrip goroutine that reads the Body can outlive
		// this handler. This can lead to a crash if the handler panics (see
		// Issue 46866). Although calling Close doesn't guarantee there isn't
		// any Read in flight after the handle returns, in practice it's safe to
		// read after closing it.
		defer outReq.Body.Close()
	}

	// create outRes by execute request
	outRes, err := r.createResponse(rw, outReq)
	if err != nil {
		r.OnError(err, rw, outReq)
		return
	}

	// Deal with 101 Switchoing Protocols response: WebSocket, h2c, etc
	if outRes.StatusCode == http.StatusSwitchingProtocols {
		if !r.modifyResponse(rw, outRes, outReq, inReq) {
			return
		}

		r.handleUpgrade(rw, outReq, outRes)
		return
	}

	// Expect connection upgrade, but not upgrade connection
	// 	request(header => Connection => Upgrade) => response(statusCode != 101)
	if upgradeType(inReq.Header) != "" {
		// get repsonse text, see what happens
		body, _ := ioutil.ReadAll(outRes.Body)
		outRes.Body.Close()

		r.OnError(fmt.Errorf("[PROXY] failed to upgrade connection (request expect upgrade, but response not allow), status: %d, error: %s", outRes.StatusCode, string(body)), rw, outReq)
		return
	}

	// http default
	// headers
	//	1. clean
	cleanResponseHeaders(outRes.Header)

	// modify response
	if !r.modifyResponse(rw, outRes, outReq, inReq) {
		return
	}

	//  2. copy
	copyHeader(rw.Header(), outRes.Header)

	//  3. trailer
	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	announcedTrailers := len(outRes.Trailer)
	if announcedTrailers > 0 {
		trailerKeys := make([]string, 0, len(outRes.Trailer))
		for k := range outRes.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		rw.Header().Add(headers.Trailer, strings.Join(trailerKeys, ", "))
	}

	// status
	rw.WriteHeader(outRes.StatusCode)

	// copy buffer
	if err := r.copyResponse(rw, outRes.Body, r.flushInterval(outRes)); err != nil {
		defer outRes.Body.Close()

		// Since we're streaming the response, if we run into an error all we can do
		// is abort the request. Issue 23643: Proxy should use ErrAbortHandler
		// on read error while copying body.
		// if !shouldPanicOnCopyError(req) {
		if !shouldPanicOnCopyError(outReq) {
			log.Printf("suppressing panic for copyResponse error in test; copy error: %v", err)
			return
		}

		panic(http.ErrAbortHandler)
	}

	outRes.Body.Close() // close now, instead of defer, to populate res.Trailer
	if len(outRes.Trailer) > 0 {
		// Force chunking if we saw a response trailer
		// This prevents net/http from calculating the length for short
		// bodies and adding a Content-Length.
		http.NewResponseController(rw).Flush()
	}

	updateResponseTrailerHeaders(rw, outRes, announcedTrailers)
}

func (r *Proxy) modifyResponse(rw http.ResponseWriter, res *http.Response, req, originReq *http.Request) bool {
	if r.OnResponse == nil {
		return true
	}

	if err := r.OnResponse(res, originReq); err != nil {
		res.Body.Close()
		r.OnError(err, rw, req)
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
	resCT := res.Header.Get(headers.ContentType)

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
		r.OnError(fmt.Errorf("backend tried to switch to invalid protocol %q", resUpType), rw, req)
	}
	if !ascii.EqualFold(reqUpType, resUpType) {
		r.OnError(fmt.Errorf("backend tried to switch protocol %q when %q was requested", resUpType, reqUpType), rw, req)
		return
	}

	hj, ok := rw.(http.Hijacker)
	if !ok {
		r.OnError(fmt.Errorf("can't switch protocols using non-Hijacker ResponseWriter type %T", rw), rw, req)
		return
	}
	backConn, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		r.OnError(fmt.Errorf("internal error: 101 switching protocols response with non-writable body"), rw, req)
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
		r.OnError(fmt.Errorf("hijack failed on protocol switch: %v", err), rw, req)
		return
	}
	defer conn.Close()

	copyHeader(rw.Header(), res.Header)

	res.Header = rw.Header()
	res.Body = nil // so res.Write only writes the headers; we have res.Body in backConn above
	if err := res.Write(brw); err != nil {
		r.OnError(fmt.Errorf("response write: %v", err), rw, req)
		return
	}
	if err := brw.Flush(); err != nil {
		r.OnError(fmt.Errorf("response flush: %v", err), rw, req)
		return
	}
	errc := make(chan error, 1)
	spc := switchProtocolCopier{user: conn, backend: backConn}
	go spc.copyToBackend(errc)
	go spc.copyFromBackend(errc)
	<-errc
}
