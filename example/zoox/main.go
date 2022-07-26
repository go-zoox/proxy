package main

import (
	"bytes"
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

			if req.Header.Get("Accept-Encoding") != "" {
				fmt.Println("req.Header.Get(\"Accept-Encoding\")", req.Header.Get("Accept-Encoding"))
				req.Header.Set("Accept-Encoding", "br")
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

			// // Inject a script tag into the page
			// fmt.Println("res.Header.Get(Content-Type)", res.Header.Get("Content-Type"))
			// if strings.Contains(res.Header.Get("Content-Type"), "text/html") {
			// 	fmt.Println("rewrite html, inject custom script")
			// 	if err := rewriteBody(res); err != nil {
			// 		return err
			// 	}
			// }

			return nil
		},
		OnHTMLResponseRewrite: func(origin []byte) ([]byte, error) {
			fmt.Println("OnHTMLResponseRewrite")
			return bytes.Replace(origin, []byte("</body>"), []byte(`
				<div>
					<script src="//cdn.jsdelivr.net/npm/eruda"></script>
					<script>eruda.init();</script>
				</div></body>`), -1), nil
		},
	})

	r.Any("/*", zoox.WrapH(p))

	r.Run(":9000")
}

// func rewriteBody(resp *http.Response) error {
// 	contentEncoding := resp.Header.Get("Content-Encoding")
// 	if contentEncoding == "" {
// 		//
// 	} else if contentEncoding == "gzip" {
// 		//
// 	} else if contentEncoding == "deflate" {
// 		//
// 	} else {
// 		fmt.Printf("unsupport content encoding: %s, ignore rewrite body \n", contentEncoding)
// 		return nil
// 	}

// 	b, err := ioutil.ReadAll(resp.Body) //Read html
// 	if err != nil {
// 		return err
// 	}
// 	err = resp.Body.Close()
// 	if err != nil {
// 		return err
// 	}

// 	fmt.Println("Content-Encoding:", resp.Header.Get("Content-Encoding"))
// 	if resp.Header.Get("Content-Encoding") == "" {
// 		b = bytes.Replace(b, []byte("</body>"), []byte(`<div>zcorky</div></body>`), -1) // replace html
// 	} else {
// 		if contentEncoding == "gzip" {
// 			fmt.Println("decompress gzip")
// 			g := gzip.New()
// 			if decodedB, err := g.Decompress(b); err != nil {
// 				return err
// 			} else {
// 				b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>zcorky</div></body>`), -1) // replace html
// 				b = g.Compress(b)
// 				fmt.Println("compress gzip success")
// 			}
// 		} else if contentEncoding == "deflate" {
// 			fmt.Println("decompress deflate")
// 			d := flate.New()
// 			if decodedB, err := d.Decompress(b); err != nil {
// 				return err
// 			} else {
// 				b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>zcorky</div></body>`), -1) // replace html
// 				b = d.Compress(b)
// 				fmt.Println("compress deflate success")
// 			}
// 		} else {
// 			return fmt.Errorf("unknown content encoding: %s", contentEncoding)
// 		}
// 	}

// 	body := ioutil.NopCloser(bytes.NewReader(b))
// 	resp.Body = body
// 	resp.ContentLength = int64(len(b))
// 	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
// 	return nil
// }
