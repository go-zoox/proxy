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
	targetX, err := url.Parse(target)
	if err != nil {
		panic(fmt.Errorf("invalid proxy target: %s", err))
	}

	cfgX := &SingleHostConfig{
		Query:           url.Values{},
		RequestHeaders:  http.Header{},
		ResponseHeaders: http.Header{},
		Rewrites:        rewriter.Rewriters{},
	}
	if len(cfg) != 0 && cfg[0] != nil {
		if cfg[0].Scheme != "" {
			targetX.Scheme = cfg[0].Scheme
		}

		if cfg[0].Rewrites != nil {
			cfgX.Rewrites = cfg[0].Rewrites
		}

		if cfg[0].Query != nil {
			cfgX.Query = cfg[0].Query
		}

		if cfg[0].RequestHeaders != nil {
			cfgX.RequestHeaders = cfg[0].RequestHeaders
		}

		if cfg[0].ResponseHeaders != nil {
			cfgX.ResponseHeaders = cfg[0].ResponseHeaders
		}

		if cfg[0].OnRequest != nil {
			cfgX.OnRequest = cfg[0].OnRequest
		}

		if cfg[0].OnResponse != nil {
			cfgX.OnResponse = cfg[0].OnResponse
		}

		if cfg[0].IsAnonymouse {
			cfgX.IsAnonymouse = true
		}

		if cfg[0].ChangeOrigin {
			cfgX.ChangeOrigin = true
		}

		if cfg[0].OnError != nil {
			cfgX.OnError = cfg[0].OnError
		}
	}

	// // host
	// if requestHeaders.Get(headers.Host) == "" {
	// 	requestHeaders.Set(headers.Host, host)
	// }
	// origin
	if cfgX.ChangeOrigin {
		if cfgX.RequestHeaders.Get(headers.Origin) != "" {
			// use target as origin
			cfgX.RequestHeaders.Set(headers.Origin, target)
		}
	}

	// user-agent
	if cfgX.RequestHeaders.Get(headers.UserAgent) == "" {
		cfgX.RequestHeaders.Set(headers.UserAgent, fmt.Sprintf("go-zoox_proxy/%s", Version))
	}

	isNeedRewrite := len(cfgX.Rewrites) != 0
	if !isNeedRewrite {
		if targetX.Path == "" || targetX.Path == "/" {
			isNeedRewrite = true
		}
	}

	return New(&Config{
		IsAnonymouse: cfgX.IsAnonymouse,
		OnRequest: func(outReq, inReq *http.Request) error {
			outReq.URL.Scheme = targetX.Scheme
			outReq.URL.Host = targetX.Host

			if isNeedRewrite {
				outReq.URL.Path = cfgX.Rewrites.Rewrite(outReq.URL.Path)
			} else {
				outReq.URL.Path = targetX.Path
			}

			if cfgX.Query != nil {
				originQuery := outReq.URL.Query()
				for k, v := range cfgX.Query {
					originQuery[k] = v
				}
				//
				outReq.URL.RawQuery = originQuery.Encode()
			}

			for k, v := range cfgX.RequestHeaders {
				outReq.Header.Set(k, v[0])
			}

			if cfgX.OnRequest != nil {
				if err := cfgX.OnRequest(outReq); err != nil {
					return err
				}
			}

			return nil
		},
		OnResponse: func(res *http.Response, originReq *http.Request) error {
			for k, v := range cfgX.ResponseHeaders {
				res.Header.Set(k, v[0])
			}

			if cfgX.OnResponse != nil {
				if err := cfgX.OnResponse(res); err != nil {
					return err
				}
			}

			return nil
		},
		OnError: cfgX.OnError,
	})
}
