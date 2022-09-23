package rewriter

import "testing"

func TestRewrite(t *testing.T) {
	url := "/api/foo/bar"
	from := "^/api/(.*)"
	to := "/$1"

	r := &Rewriter{from, to}
	if !r.IsMatch(url) {
		t.Errorf("%s should match %s", url, from)
	}

	if r.Rewrite(url) != "/foo/bar" {
		t.Errorf("%s should be rewritten to %s", url, to)
	}
}

func TestRewrites(t *testing.T) {
	rs := Rewriters{
		Rewriter{"^/api/foo/(.*)", "/$1"},
		Rewriter{"^/api/(.*)", "/$1"},
	}

	url := "/api/foo/bar"
	if rs.Rewrite(url) != "/bar" {
		t.Errorf("%s should be rewritten to %s", url, "/bar")
	}
}
