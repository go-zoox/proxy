package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-zoox/proxy"
	"github.com/go-zoox/proxy/utils/rewriter"
)

func main() {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Hello, world!"))
	})

	p := proxy.NewSingleTarget("https://httpbin.zcorky.com", &proxy.SingleTargetConfig{
		// Rewrites: map[string]string{
		// 	"^/api/(.*)": "/$1",
		// },
		Rewrites: rewriter.Rewriters{
			{"^/api/(.*)", "/$1"},
		},
	})

	r.Get("/api/*", func(w http.ResponseWriter, r *http.Request) {
		p.ServeHTTP(w, r)
	})

	http.ListenAndServe(":8080", r)
}
