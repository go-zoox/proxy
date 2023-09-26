package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptrace"
	"net/textproto"

	"github.com/go-zoox/headers"
	"github.com/go-zoox/proxy/utils/ascii"
)

func (r *Proxy) createRequest(ctx context.Context, rw http.ResponseWriter, inReq *http.Request) (*http.Request, error) {
	outReq := inReq.Clone(ctx)

	// Issue 16036: nil Body for http.Transport retries
	if inReq.ContentLength == 0 && outReq.Body != nil {
		outReq.Body = nil
	}

	// Issue 33142: historical behavior was to always allocate
	if outReq.Header == nil {
		outReq.Header = make(http.Header)
	}

	if err := r.OnRequest(outReq, inReq); err != nil {
		return nil, err
	}

	// default http
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}

	outReq.Close = false

	// get upgrade
	upgrade := getUpgradeType(&outReq.Header)
	if !ascii.IsPrint(upgrade) {
		return nil, &HTTPError{http.StatusBadRequest, fmt.Sprintf("client tried to switch to invalid protocol %q", upgrade)}
	}

	// clean headers
	cleanRequestHeaders(outReq.Header, inReq)
	// add headers
	addRequestHeaders(outReq.Header, inReq, r.isAnonymouse)
	// upgrade header
	updateRequestUpgradeHeaders(outReq.Header, upgrade)
	// X-Forwarded-For
	updateRequestXForwardedForHeader(outReq.Header, outReq, r.isAnonymouse)

	//
	if _, ok := outReq.Header[headers.UserAgent]; !ok {
		// If the outbound request doesn't have a User-Agent header set,
		// don't send the default Go HTTP client User-Agent.
		outReq.Header.Set(headers.UserAgent, "")
	}

	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			h := rw.Header()
			copyHeader(h, http.Header(header))
			rw.WriteHeader(code)

			// Clear headers, it's not automatically done by ResponseWriter.WriteHeader() for 1xx responses

			// @TODO go1.21
			// clear(h)

			// @TODO < go1.21
			for k := range h {
				delete(h, k)
			}
			return nil
		},
	}
	outReq = outReq.WithContext(httptrace.WithClientTrace(outReq.Context(), trace))

	// // @BUG fix header host
	// // issue: https://github.com/golang/go/issues/28168
	// if outReq.Header.Get(headers.Host) != "" {
	// 	outReq.Host = outReq.Header.Get(headers.Host)
	// }

	return outReq, nil
}
