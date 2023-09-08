package proxy

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-zoox/headers"
	"github.com/go-zoox/proxy/utils/rewriter"
)

// SingleHostConfig is the configuration for SingleTarget.
type SingleHostConfig struct {
	Rewrites        rewriter.Rewriters
	Scheme          string
	Query           url.Values
	RequestHeaders  http.Header
	ResponseHeaders http.Header
	OnRequest       func(req *http.Request) error
	OnResponse      func(res *http.Response) error
	//
	IsAnonymouse bool
	ChangeOrigin bool
	//
	OnError func(err error, rw http.ResponseWriter, req *http.Request)
}

// NewSingleHost creates a new Single Host Proxy.
// target is the URL of the host you wish to proxy to.
// cfg is the configuration for the SingleHost.
//   - Rewrites is the rewriters for the SingleHost.
//   - Scheme overrides the scheme of target.
//   - Query is the query of the SingleHost.
//   - RequestHeaders is the request headers of the SingleHost.
//   - ResponseHeaders is the response headers of the SingleHost.
//   - OnRequest is the hook that is called before the request is sent.
//   - OnResponse is the hook that is called after the response is received.
//   - IsAnonymouse is a flag to indicate whether the proxy is anonymouse.
//     which means the proxy will not add headers:
//     X-Forwarded-For
//     X-Forwarded-Proto
//     X-Forwarded-Host
//     X-Forwarded-Port
//     Default is false.
//   - ChangeOrigin is a flag to indicate whether the proxy will change the origin.
//     which means the proxy will change the origin to target.
//     Default is false.
//   - OnError is the hook that is called when an error occurs.
//
// Example:
//
//	// All requests will be redirected to https://httpbin.org
//	NewSingleHost("https://httpbin.org")
//
//	// All requests will be redirected to https://httpbin.org/ip
//	NewSingleHost("https://httpbin.org/ip")
//
//	// All requests will be redirected to https://httpbin.org with Rewrites
//	 NewSingleHost("https://httpbin.org", &SingleHostConfig{
//	 	Rewrites: rewriter.Rewriters{
//	 		{ From: "/api/get", To: "/get" },
//		  { From: "/api/post", To: "/post" },
//	 	  { From: "/api/v2/(.*)", To: "/$1" },
//	 	},
//	 	Query: url.Values{
//	 		"foo": []string{"bar"},
//	 	},
//	 	RequestHeaders: http.Header{
//	 		headers.Host: []string{"httpbin.org"},
//	 	},
//	 	ResponseHeaders: http.Header{
//	 		headers.Server: []string{"go-zoox_proxy"},
//	 	},
//	 	OnRequest: func(req *http.Request) error {
//	 		// do something
//	 		return nil
//	 	},
//	 	OnResponse: func(res *http.Response) error {
//	 		// do something
//	 		return nil
//	 	},
//	 	IsAnonymouse: true,
//	 	ChangeOrigin: true,
//	 	OnError: func(err error, rw http.ResponseWriter, req *http.Request) {
//	 		// do something
//	 	},
//	 })
func NewSingleHost(target string, cfg ...*SingleHostConfig) *Proxy {
	var onRequest func(req *http.Request) error
	var onResponse func(res *http.Response) error
	var query url.Values
	var requestHeaders = make(http.Header)
	var responseHeaders http.Header
	var isAnonymouse bool
	var changeOrigin bool
	var onError func(err error, rw http.ResponseWriter, req *http.Request)
	var rewriters rewriter.Rewriters

	targetX, err := url.Parse(target)
	if err != nil {
		panic(fmt.Errorf("invalid proxy target: %s", err))
	}

	if len(cfg) > 0 {
		if cfg[0].Rewrites != nil {
			rewriters = cfg[0].Rewrites
		}
		if cfg[0].Scheme != "" {
			targetX.Scheme = cfg[0].Scheme
		}
		if cfg[0].Query != nil {
			query = cfg[0].Query
		}
		if cfg[0].RequestHeaders != nil {
			requestHeaders = cfg[0].RequestHeaders
		}
		if cfg[0].ResponseHeaders != nil {
			responseHeaders = cfg[0].ResponseHeaders
		}
		if cfg[0].OnRequest != nil {
			onRequest = cfg[0].OnRequest
		}
		if cfg[0].OnResponse != nil {
			onResponse = cfg[0].OnResponse
		}
		if cfg[0].IsAnonymouse {
			isAnonymouse = true
		}
		if cfg[0].ChangeOrigin {
			changeOrigin = true
		}
		if cfg[0].OnError != nil {
			onError = cfg[0].OnError
		}
	}

	// // host
	// if requestHeaders.Get(headers.Host) == "" {
	// 	requestHeaders.Set(headers.Host, host)
	// }
	// origin
	if changeOrigin {
		if requestHeaders.Get(headers.Origin) != "" {
			// use target as origin
			requestHeaders.Set(headers.Origin, target)
		}
	}

	// user-agent
	if requestHeaders.Get(headers.UserAgent) == "" {
		requestHeaders.Set(headers.UserAgent, fmt.Sprintf("go-zoox_proxy/%s", Version))
	}

	isRewriteExist := len(rewriters) != 0

	return New(&Config{
		IsAnonymouse: isAnonymouse,
		OnRequest: func(outReq, inReq *http.Request) error {
			outReq.URL.Scheme = targetX.Scheme
			outReq.URL.Host = targetX.Host

			if isRewriteExist {
				outReq.URL.Path = rewriters.Rewrite(outReq.URL.Path)
			} else {
				outReq.URL.Path = targetX.Path
			}

			if query != nil {
				originQuery := outReq.URL.Query()
				for k, v := range query {
					originQuery[k] = v
				}
				outReq.URL.RawQuery = originQuery.Encode()
			}

			for k, v := range requestHeaders {
				outReq.Header.Set(k, v[0])
			}

			if onRequest != nil {
				if err := onRequest(outReq); err != nil {
					return err
				}
			}

			return nil
		},
		OnResponse: func(res *http.Response, originReq *http.Request) error {
			for k, v := range responseHeaders {
				res.Header.Set(k, v[0])
			}

			if onResponse != nil {
				if err := onResponse(res); err != nil {
					return err
				}
			}

			return nil
		},
		OnError: onError,
	})
}
