package proxy

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSingleTarget(t *testing.T) {
	p := NewSingleTarget("https://httpbin.zcorky.com", &SingleTargetConfig{
		// Scheme: "https",
		Query: url.Values{
			"foo": []string{"bar"},
		},
		RequestHeaders: http.Header{
			// "Host":            "httpbin.zcorky.com",
			"x-custom-header": []string{"custom"},
		},
		ResponseHeaders: http.Header{
			"x-custom-response": []string{"custom"},
		},
	})

	req := httptest.NewRequest("GET", "/get?foo2=bar2", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	res := w.Result()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", res.StatusCode)
	}
	if res.Header.Get("x-custom-response") != "custom" {
		t.Errorf("Expected custom, got %s", res.Header.Get("x-custom-response"))
	}

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	json := gjson.Parse(string(data))

	if json.Get("headers.host").String() != "httpbin.zcorky.com" {
		t.Errorf("Expected headers.host to be httpbin.zcorky.com, got %s", json.Get("headers.host").String())
	}

	if json.Get("headers.x-custom-header").String() != "custom" {
		t.Errorf("Expected headers.x-custom-header to be custom, got %s", json.Get("headers.x-custom-header").String())
	}

	if json.Get("headers.x-real-ip").String() == "" {
		t.Errorf("Expected headers.x-real-ip to be set, got empty")
	}

	if json.Get("headers.x-forwarded-for").String() == "" {
		t.Errorf("Expected headers.x-forwarded-for to be set, got empty")
	}

	if json.Get("headers.x-forwarded-proto").String() == "" {
		t.Errorf("Expected headers.x-forwarded-proto to be set, got empty")
	}

	if json.Get("headers.x-forwarded-host").String() == "" {
		t.Errorf("Expected headers.x-forwarded-host to be set, got empty")
	}

	if json.Get("headers.x-forwarded-port").String() == "" {
		t.Errorf("Expected headers.x-forwarded-port to be set, got empty")
	}

	if json.Get("query.foo").String() != "bar" {
		t.Errorf("Expected query.foo to be bar, got %s", json.Get("query.foo").String())
	}

	if json.Get("query.foo2").String() != "bar2" {
		t.Errorf("Expected query.foo2 to be bar2, got %s", json.Get("query.foo2").String())
	}
}

func TestSingleTargetRewrites(t *testing.T) {
	p := NewSingleTarget("https://httpbin.zcorky.com", &SingleTargetConfig{
		//
		Rewrites: map[string]string{
			// "/api/v1/uuid": "/uuid",
			"/api/v1/(.*)": "/$1",
		},
	})

	req := httptest.NewRequest("GET", "/api/v1/uuid", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	res := w.Result()
	if res.StatusCode != 200 {
		t.Errorf("Expected 200, got %d", res.StatusCode)
	}

	defer res.Body.Close()
	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}
	json := gjson.Parse(string(data))

	if json.Get("uuid").String() == "" {
		t.Errorf("Expected uuid to be set, got empty")
	}
}
