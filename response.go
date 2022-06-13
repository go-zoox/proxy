package proxy

import (
	"net/http"
)

func (r *Proxy) createResponse(rw http.ResponseWriter, req *http.Request) (*http.Response, error) {
	transport := http.DefaultTransport

	// execute request
	res, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}
