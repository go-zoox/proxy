package main

import (
	"fmt"
	"net/http"

	"regexp"

	"github.com/go-zoox/proxy"
	"github.com/go-zoox/zoox"
	zd "github.com/go-zoox/zoox/default"
)

func main() {
	r := zd.Default()

	r.Get("/", func(ctx *zoox.Context) {
		ctx.JSON(200, zoox.H{
			"hello": "world",
		})
	})

	localHostRe := regexp.MustCompile(`^http://127.0.0.1:9000/(.*)$`)
	remoteHostRe := regexp.MustCompile(`^https://github.com/(.*)$`)
	setCookieRe := regexp.MustCompile(`Domain=github.com`)
	p := proxy.NewSingleTarget("https://github.com", &proxy.SingleTargetConfig{
		Rewrites: map[string]string{
			"^/(.*)": "/$1",
		},
		OnRequest: func(req *http.Request) error {
			if req.Header.Get("Origin") != "" {
				fmt.Println("Origin:", req.Header.Get("Origin"))
				req.Header.Set("Origin", "https://github.com")
			}

			if req.Header.Get("Host") != "" {
				fmt.Println("Host:", req.Header.Get("Host"))
				req.Header.Set("Host", "github.com")
			}

			if req.Header.Get("Referer") != "" {
				fmt.Println("req.Referer()", req.Referer())
				req.Header.Set("Referer", localHostRe.ReplaceAllString(req.Referer(), "https://github.com/$1"))
			}

			return nil
		},
		OnResponse: func(res *http.Response) error {
			location := res.Header.Get("Location")
			if location != "" {
				res.Header.Set("Location", remoteHostRe.ReplaceAllString(location, "http://127.0.0.1:9000/$1"))
			}

			setCookie := res.Header.Get("Set-Cookie")
			if setCookie != "" {
				fmt.Println("setCookie", setCookie)
				res.Header.Set("Set-Cookie", setCookieRe.ReplaceAllString(setCookie, "Domain=127.0.0.1"))
			}

			res.Header.Del("Content-Security-Policy")

			return nil
		},
	})

	r.Any("/*", zoox.WrapH(p))

	r.Run(":9000")
}
