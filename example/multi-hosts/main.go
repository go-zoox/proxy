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

// visit http://127.0.0.1:9999/get => http://127.0.0.1:8080/get
// curl -v http://127.0.0.1:9999/get
