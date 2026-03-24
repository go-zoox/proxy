package proxy

import (
	"testing"
)

func TestRouteResolver_PreferExactHost(t *testing.T) {
	cfg := &MultiHostsConfig{
		Routes: []MultiHostsRoute{
			{
				Host: ".*",
				Backend: MultiHostsRouteBackend{
					ServiceName: "regex-backend",
				},
			},
			{
				Host: "api.example.com",
				Backend: MultiHostsRouteBackend{
					ServiceName: "exact-backend",
				},
			},
		},
	}

	resolver, err := newRouteResolver(cfg)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}

	route, err := resolver.Resolve("api.example.com")
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}

	if route.Backend.ServiceName != "exact-backend" {
		t.Fatalf("expected exact-backend, got %s", route.Backend.ServiceName)
	}
}

func TestRouteResolver_RegexFallback(t *testing.T) {
	cfg := &MultiHostsConfig{
		Routes: []MultiHostsRoute{
			{
				Host: "^.+\\.example\\.com$",
				Backend: MultiHostsRouteBackend{
					ServiceName: "regex-backend",
				},
			},
		},
	}

	resolver, err := newRouteResolver(cfg)
	if err != nil {
		t.Fatalf("build resolver: %v", err)
	}

	route, err := resolver.Resolve("foo.example.com")
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}

	if route.Backend.ServiceName != "regex-backend" {
		t.Fatalf("expected regex-backend, got %s", route.Backend.ServiceName)
	}
}

func BenchmarkRouteResolverResolve_Exact(b *testing.B) {
	cfg := &MultiHostsConfig{
		Routes: []MultiHostsRoute{
			{
				Host: "api.example.com",
				Backend: MultiHostsRouteBackend{
					ServiceName: "exact-backend",
				},
			},
			{
				Host: "^.+\\.example\\.com$",
				Backend: MultiHostsRouteBackend{
					ServiceName: "regex-backend",
				},
			},
		},
	}

	resolver, err := newRouteResolver(cfg)
	if err != nil {
		b.Fatalf("build resolver: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := resolver.Resolve("api.example.com"); err != nil {
			b.Fatalf("resolve route: %v", err)
		}
	}
}

func BenchmarkRouteResolverResolve_Regex(b *testing.B) {
	cfg := &MultiHostsConfig{
		Routes: []MultiHostsRoute{
			{
				Host: "api.example.com",
				Backend: MultiHostsRouteBackend{
					ServiceName: "exact-backend",
				},
			},
			{
				Host: "^.+\\.example\\.com$",
				Backend: MultiHostsRouteBackend{
					ServiceName: "regex-backend",
				},
			},
		},
	}

	resolver, err := newRouteResolver(cfg)
	if err != nil {
		b.Fatalf("build resolver: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := resolver.Resolve("foo.example.com"); err != nil {
			b.Fatalf("resolve route: %v", err)
		}
	}
}
