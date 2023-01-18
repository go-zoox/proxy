package proxy

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/go-zoox/headers"
	"github.com/go-zoox/proxy/utils/rewriter"
)

// SingleTargetConfig is the configuration for SingleTarget.
type SingleTargetConfig struct {
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
}

// NewSingleTarget creates a new SingleTarget Proxy.
func NewSingleTarget(target string, cfg ...*SingleTargetConfig) *Proxy {
	var onRequest func(req *http.Request) error
	var onResponse func(res *http.Response) error
	var query url.Values
	var requestHeaders = make(http.Header)
	var responseHeaders http.Header
	var isAnonymouse bool
	var changeOrigin bool

	host := target
	scheme := "http"
	rewriters := rewriter.Rewriters{}

	if re, err := regexp.Compile(`^(.+)://([^/]+)`); err != nil {
		panic(fmt.Errorf("regexp compile error: %s", err))
	} else {
		if matched := re.FindStringSubmatch(target); matched != nil {
			scheme = matched[1]
			host = matched[2]
		} else {
			panic(fmt.Errorf("invalid proxy target: %s", target))
		}
	}

	if len(cfg) > 0 {
		if cfg[0].Rewrites != nil {
			rewriters = cfg[0].Rewrites
		}
		if cfg[0].Scheme != "" {
			scheme = cfg[0].Scheme
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
	}

	// host
	if requestHeaders.Get(headers.Host) == "" {
		requestHeaders.Set(headers.Host, host)
	}
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

	return New(&Config{
		IsAnonymouse: isAnonymouse,
		OnRequest: func(req *http.Request) error {
			req.URL.Scheme = scheme
			req.URL.Host = host
			req.URL.Path = rewriters.Rewrite(req.URL.Path)

			if query != nil {
				originQuery := req.URL.Query()
				for k, v := range query {
					originQuery[k] = v
				}
				req.URL.RawQuery = originQuery.Encode()
			}

			for k, v := range requestHeaders {
				req.Header.Set(k, v[0])
			}

			if onRequest != nil {
				if err := onRequest(req); err != nil {
					return err
				}
			}

			return nil
		},
		OnResponse: func(res *http.Response) error {
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
	})
}
