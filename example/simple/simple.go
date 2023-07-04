package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")

	http.ListenAndServe(":9999", proxy.New(&proxy.Config{
		OnRequest: func(req, originReq *http.Request) error {
			req.URL.Host = "127.0.0.1:8080"
			return nil
		},
	}))
}

// visit http://127.0.0.1:9999/ip => http://127.0.0.1:8080/ip
// curl -v http://127.0.0.1:9999/ip
