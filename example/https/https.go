package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")
	http.ListenAndServe(":9999", proxy.NewSingleTarget("https://httpbin.zcorky.com", &proxy.SingleTargetConfig{
		RequestHeaders: http.Header{
			"x-custom-header": []string{"custom"},
		},
	}))
}

// visit http://127.0.0.1:9999/get => https://httpbin.zcorky.com/get
// curl -v http://127.0.0.1:9999/get
