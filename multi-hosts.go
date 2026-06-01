package proxy

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-zoox/headers"
	"github.com/go-zoox/logger"
	"github.com/go-zoox/proxy/utils/rewriter"
)

// MultiHostsConfig ...
type MultiHostsConfig struct {
	Routes []MultiHostsRoute `json:"routes"`
	// EnableAccessLog controls request logging in the hot path.
	// Default is false to avoid logging overhead.
	EnableAccessLog bool `json:"enable_access_log"`
	// TrustProxy controls whether to trust upstream X-Forwarded-* headers.
	// When true, upstream X-Forwarded-* has higher priority than TLS/fallback.
	// Default is false.
	TrustProxy bool `json:"trust_proxy"`
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

type routeContextKey struct{}

type regexRoute struct {
	pattern *regexp.Regexp
	route   *MultiHostsRoute
}

type routeResolver struct {
	exact map[string]*MultiHostsRoute
	regex []regexRoute
}

func newRouteResolver(cfg *MultiHostsConfig) (*routeResolver, error) {
	resolver := &routeResolver{
		exact: map[string]*MultiHostsRoute{},
		regex: make([]regexRoute, 0, len(cfg.Routes)),
	}

	for i := range cfg.Routes {
		route := &cfg.Routes[i]
		if _, ok := resolver.exact[route.Host]; !ok {
			resolver.exact[route.Host] = route
		}

		pattern, err := regexp.Compile(route.Host)
		if err != nil {
			return nil, fmt.Errorf("invalid route host pattern(%s): %w", route.Host, err)
		}

		resolver.regex = append(resolver.regex, regexRoute{
			pattern: pattern,
			route:   route,
		})
	}

	return resolver, nil
}

func (r *routeResolver) Resolve(hostname string) (*MultiHostsRoute, error) {
	if route, ok := r.exact[hostname]; ok {
		return route, nil
	}

	for _, candidate := range r.regex {
		if candidate.pattern.MatchString(hostname) {
			return candidate.route, nil
		}
	}

	return nil, fmt.Errorf("route(%s) not found", hostname)
}

// NewMultiHosts ...
func NewMultiHosts(cfg *MultiHostsConfig) *Proxy {
	resolver, err := newRouteResolver(cfg)
	if err != nil {
		panic(err)
	}

	return New(&Config{
		IsAnonymouse: false,
		TrustProxy:   cfg.TrustProxy,
		OnRequest: func(req, originReq *http.Request) error {
			hostname := getHostname(originReq)
			route, err := resolver.Resolve(hostname)
			if err != nil {
				return err
			}
			*req = *req.WithContext(context.WithValue(req.Context(), routeContextKey{}, route))

			req.URL.Scheme = route.Backend.ServiceProtocol
			if req.URL.Scheme == "" {
				req.URL.Scheme = "http"
			}

			req.URL.Host = fmt.Sprintf("%s:%d", route.Backend.ServiceName, route.Backend.ServicePort)
			req.URL.Path = route.Backend.Rewriters.Rewrite(req.URL.Path)

			if cfg.EnableAccessLog {
				logger.Infof("[go-zoox.proxy][%s][%s => %s://%s] %s %s", req.RemoteAddr, hostname, req.URL.Scheme, req.URL.Host, req.Method, req.URL.Path)
			}

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
			route, ok := res.Request.Context().Value(routeContextKey{}).(*MultiHostsRoute)
			if !ok || route == nil {
				return fmt.Errorf("route context missing")
			}

			for k, v := range route.Backend.ResponseHeaders {
				res.Header.Set(k, v[0])
			}

			return nil
		},
	})
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
