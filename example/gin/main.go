package main

import (
	"github.com/gin-gonic/gin"
	"github.com/go-zoox/proxy"
	"github.com/go-zoox/proxy/utils/rewriter"
)

func main() {
	r := gin.Default()

	r.GET("/", func(ctx *gin.Context) {
		ctx.JSON(200, gin.H{
			"hello": "world",
		})
	})

	p := proxy.NewSingleTarget("https://httpbin.zcorky.com", &proxy.SingleTargetConfig{
		// Rewrites: map[string]string{
		// 	"^/api/(.*)": "/$1",
		// },
		Rewrites: rewriter.Rewriters{
			{"^/api/(.*)", "/$1"},
		},
	})

	// r.GET("/api/*path", func(ctx *gin.Context) {
	// 	// ctx.Request.URL.Path = re.ReplaceAllString(ctx.Request.URL.Path, "/$1")
	// 	p.ServeHTTP(ctx.Writer, ctx.Request)
	// })

	r.GET("/api/*path", gin.WrapH(p))

	r.Run()
}
