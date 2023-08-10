package main

import (
	"fmt"
	"net/http"

	"github.com/go-zoox/proxy"
)

func main() {
	fmt.Println("Starting proxy at http://127.0.0.1:9999 ...")
	http.ListenAndServe(":9999", proxy.NewSingleHost("http://127.0.0.1:8080", &proxy.SingleHostConfig{}))
}

// visit http://127.0.0.1:9999/get => http://127.0.0.1:8080/get
// curl -v http://127.0.0.1:9999/get
