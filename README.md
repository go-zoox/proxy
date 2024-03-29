# Proxy - Make Reverse Proxy easier to use

[![PkgGoDev](https://pkg.go.dev/badge/github.com/go-zoox/proxy)](https://pkg.go.dev/github.com/go-zoox/proxy)
[![Build Status](https://github.com/go-zoox/proxy/actions/workflows/lint.yml/badge.svg?branch=master)](https://github.com/go-zoox/proxy/actions/workflows/lint.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-zoox/proxy)](https://goreportcard.com/report/github.com/go-zoox/proxy)
[![Coverage Status](https://coveralls.io/repos/github/go-zoox/proxy/badge.svg?branch=master)](https://coveralls.io/github/go-zoox/proxy?branch=master)
[![GitHub issues](https://img.shields.io/github/issues/go-zoox/proxy.svg)](https://github.com/go-zoox/proxy/issues)
[![Release](https://img.shields.io/github/tag/go-zoox/proxy.svg?label=Release)](https://github.com/go-zoox/proxy/tags)


## Installation
To install the package, run:
```bash
go get -u github.com/go-zoox/proxy
```

## Quick Start

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")

	http.ListenAndServe(":9999", proxy.New(&proxy.Config{
		OnRequest: func(req *http.Request) error {
			req.URL.Host = "127.0.0.1:8080"
			return nil
		},
	}))
}

// visit http://127.0.0.1:9999/ip => http://127.0.0.1:8080/ip
// curl -v http://127.0.0.1:9999/ip
```

## Best Practice

### 1. Single Host => All traffic to a single target with path

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	target := "https://httpbin.org"

	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")
	http.ListenAndServe(":9999", proxy.NewSingleHost(target))
}
```

### 2. Single Host => All traffic to a single target with rewrite

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
	"github.com/go-zoox/proxy/utils/rewriter"
)

func main() {
	target := "https://httpbin.org"

	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")
	http.ListenAndServe(":9999", proxy.NewSingleHost(target, &proxy.SingleHostConfig{
		Rewrites: rewriter.Rewriters{
			{
				From: "/api/ip",
				To:   "/ip",
			},
			{
				From: "/api/headers",
				To:   "/headers",
			},
			{
				From: "/api/v2/(.*)",
				To:   "/$1",
			},
		},
	}))
}
```

### 3. Multiple Hosts => All traffic to multiple targets

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")
	
	http.ListenAndServe(":9999", proxy.NewMultiHosts(&proxy.MultiHostsConfig{
		Routes: []proxy.MultiHostsRoute{
			{
				Host: "httpbin1.go-zoox.work",
				Backend: proxy.MultiHostsRouteBackend{
					ServiceProtocol: "https",
					ServiceName:     "httpbin.zcorky.com",
					ServicePort:     443,
				},
			},
			{
				Host: "httpbin2.go-zoox.work",
				Backend: proxy.MultiHostsRouteBackend{
					ServiceProtocol: "https",
					ServiceName:     "httpbin.org",
					ServicePort:     443,
				},
			},
		},
	}))
}
```

## Inspiration
* Go httputil.ReverseProxy

## License
GoZoox is released under the [MIT License](./LICENSE).
