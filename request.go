package proxy

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy/utils/ascii"
)

func (r *Proxy) createRequest(ctx context.Context, rw http.ResponseWriter, originReq *http.Request) (*http.Request, error) {
	newReq := originReq.Clone(ctx)

	// Issue 16036: nil Body for http.Transport retries
	if originReq.ContentLength == 0 && newReq.Body != nil {
		newReq.Body = nil
	}

	// Issue 33142: historical behavior was to always allocate
	if newReq.Header == nil {
		newReq.Header = make(http.Header)
	}

	if err := r.onRequest(newReq); err != nil {
		return nil, err
	}

	// default http
	if newReq.URL.Scheme == "" {
		newReq.URL.Scheme = "http"
	}

	newReq.Close = false

	// get upgrade
	upgrade := getUpgradeType(&newReq.Header)
	if !ascii.IsPrint(upgrade) {
		r.onError(&HTTPError{http.StatusBadRequest, fmt.Sprintf("unsupported upgrade type: %s", upgrade)}, rw, newReq)
	}

	// clean headers
	cleanRequestHeaders(newReq.Header)
	// add headers
	addRequestHeaders(newReq.Header, originReq, r.isAnonymouse)
	// upgrade header
	updateRequestUpgradeHeaders(newReq.Header, upgrade)
	// X-Forwarded-For
	updateRequestXForwardedForHeader(newReq.Header, newReq, r.isAnonymouse)

	// @BUG fix header host
	// issue: https://github.com/golang/go/issues/28168
	if newReq.Header.Get("host") != "" {
		newReq.Host = newReq.Header.Get("host")
	}

	return newReq, nil
}
