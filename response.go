package proxy

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-zoox/compress/flate"
	"github.com/go-zoox/compress/gzip"
	"github.com/go-zoox/headers"
)

func (r *Proxy) createResponse(rw http.ResponseWriter, req *http.Request) (*http.Response, error) {
	transport := http.DefaultTransport

	// execute request
	res, err := transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func rewriteHTMLResponse(resp *http.Response, onRewrite func([]byte) ([]byte, error)) error {
	contentEncoding := resp.Header.Get(headers.ContentEncoding)
	if contentEncoding == "" {
		//
	} else if contentEncoding == "gzip" {
		//
	} else if contentEncoding == "deflate" {
		//
	} else {
		fmt.Printf("unsupport content encoding: %s, ignore rewrite body\n", contentEncoding)
		return nil
	}

	b, err := ioutil.ReadAll(resp.Body) //Read html
	if err != nil {
		return err
	}
	err = resp.Body.Close()
	if err != nil {
		return err
	}

	if resp.Header.Get(headers.ContentEncoding) == "" {
		// replace html
		// like nginx sub_filter
		// example: b = bytes.Replace(b, []byte("</body>"), []byte(`<div>custom</div></body>`), -1)
		b, err = onRewrite(b)
		if err != nil {
			return err
		}
	} else {
		if contentEncoding == "gzip" {
			g := gzip.New()
			decodedB, err := g.Decompress(b)
			if err != nil {
				return err
			}

			// replace html
			// like nginx sub_filter
			// example: b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>custom</div></body>`), -1) // replace html
			b, err = onRewrite(decodedB)
			if err != nil {
				return err
			}
			b = g.Compress(b)
		} else if contentEncoding == "deflate" {
			d := flate.New()
			decodedB, err := d.Decompress(b)
			if err != nil {
				return err
			}

			// replace html
			// like nginx sub_filter
			// example: b = bytes.Replace(decodedB, []byte("</body>"), []byte(`<div>custom</div></body>`), -1) // replace html
			b, err = onRewrite(decodedB)
			if err != nil {
				return err
			}

			b = d.Compress(b)
		} else {
			return fmt.Errorf("unsupport content encoding: %s", contentEncoding)
		}
	}

	body := ioutil.NopCloser(bytes.NewReader(b))
	resp.Body = body
	resp.ContentLength = int64(len(b))
	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return nil
}

// CreateOnHTMLRewriteResponse create a function to rewrite html response
func CreateOnHTMLRewriteResponse(fn func(origin []byte, res *http.Response) ([]byte, error)) func(*http.Response) error {
	return func(res *http.Response) error {
		if strings.Contains(res.Header.Get(headers.ContentType), "text/html") {
			if err := rewriteHTMLResponse(res, func(b []byte) ([]byte, error) {
				return fn(b, res)
			}); err != nil {
				return err
			}
		}

		return nil
	}
}

// CreateOnInjectScriptsResponse create a function to inject scripts
func CreateOnInjectScriptsResponse(fn func(origin []byte, res *http.Response) string) func(*http.Response) error {
	return CreateOnHTMLRewriteResponse(func(b []byte, res *http.Response) ([]byte, error) {
		scripts := fn(b, res)
		if scripts == "" {
			return b, nil
		}

		return bytes.Replace(b, []byte("</body>"), []byte(fmt.Sprintf(`%s</body>`, scripts)), -1), nil
	})
}
