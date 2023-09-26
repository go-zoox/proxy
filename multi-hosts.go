package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-zoox/cache"
	"github.com/go-zoox/core-utils/regexp"
	"github.com/go-zoox/headers"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/proxy/utils/rewriter"
)

// MultiHostsConfig ...
type MultiHostsConfig struct {
	Routes []MultiHostsRoute `json:"routes"`
}

// MultiHostsRoute ...
type MultiHostsRoute struct {
	Host    string                 `json:"host"`
	Backend MultiHostsRouteBackend `json:"backend"`
}

// MultiHostsRouteBackend ...
type MultiHostsRouteBackend struct {
	ServiceProtocol string `json:"service_protocol"`
	ServiceName     string `json:"service_name"`
	ServicePort     int64  `json:"service_port"`
	// Request
	Rewriters rewriter.Rewriters `json:"rewriters"`
	Headers   http.Header        `json:"headers"`
	//
	ResponseHeaders http.Header `json:"response_headers"`
}

// NewMultiHosts ...
func NewMultiHosts(cfg *MultiHostsConfig) *Proxy {
	return New(&Config{
		IsAnonymouse: false,
		OnContext: func(ctx context.Context) (context.Context, error) {
			return context.WithValue(ctx, stateKey, cache.New()), nil
		},
		OnRequest: func(req, originReq *http.Request) error {
			state := req.Context().Value(stateKey).(cache.Cache)
			hostname := getHostname(originReq)
			route, err := getRoute(cfg, hostname)
			if err != nil {
				return err
			}
			if err := state.Set("route", route); err != nil {
				return err
			}

			req.URL.Scheme = route.Backend.ServiceProtocol
			if req.URL.Scheme == "" {
				req.URL.Scheme = "http"
			}

			req.URL.Host = fmt.Sprintf("%s:%d", route.Backend.ServiceName, route.Backend.ServicePort)
			req.URL.Path = route.Backend.Rewriters.Rewrite(req.URL.Path)

			logger.Infof("[%s][%s => %s://%s] %s %s", req.RemoteAddr, hostname, req.URL.Scheme, req.URL.Host, req.Method, req.URL.Path)

			for k, v := range route.Backend.Headers {
				req.Header.Set(k, v[0])
			}

			// origin
			switch route.Backend.ServicePort {
			case 80, 443:
				req.Header.Set(headers.Host, route.Backend.ServiceName)
			default:
				req.Header.Set(headers.Host, req.URL.Host)
			}

			return nil
		},
		OnResponse: func(res *http.Response, originReq *http.Request) error {
			state := res.Request.Context().Value(stateKey).(cache.Cache)
			route := &MultiHostsRoute{}
			if err := state.Get("route", route); err != nil {
				return err
			}

			for k, v := range route.Backend.ResponseHeaders {
				res.Header.Set(k, v[0])
			}

			return nil
		},
	})
}

func getRoute(cfg *MultiHostsConfig, hostname string) (*MultiHostsRoute, error) {
	for _, route := range cfg.Routes {
		if ok := regexp.Match(route.Host, hostname); ok {
			return &route, nil
		}
	}

	return nil, fmt.Errorf("route(%s) not found", hostname)
}

func getHostname(req *http.Request) string {
	host, _ := splitHostPort(req.Host)
	return host
}

// splitHostPort separates host and port. If the port is not valid, it returns
// the entire input as host, and it doesn't check the validity of the host.
// Unlike net.SplitHostPort, but per RFC 3986, it requires ports to be numeric.
func splitHostPort(hostPort string) (host, port string) {
	host = hostPort

	colon := strings.LastIndexByte(host, ':')
	if colon != -1 && validOptionalPort(host[colon:]) {
		host, port = host[:colon], host[colon+1:]
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return
}

// validOptionalPort reports whether port is either an empty string
// or matches /^:\d*$/
func validOptionalPort(port string) bool {
	if port == "" {
		return true
	}
	if port[0] != ':' {
		return false
	}
	for _, b := range port[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}
