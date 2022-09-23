package rewriter

import "regexp"

type Rewriter struct {
	From string
	To   string
}

func (r *Rewriter) re() *regexp.Regexp {
	return regexp.MustCompile(r.From)
}

func (r *Rewriter) IsMatch(path string) bool {
	return r.re().MatchString(path)
}

func (r *Rewriter) Rewrite(path string) string {
	return r.re().ReplaceAllString(path, r.To)
}

type Rewriters []Rewriter

func (r *Rewriters) Rewrite(path string) string {
	for _, rewriter := range *r {
		if rewriter.IsMatch(path) {
			return rewriter.Rewrite(path)
		}
	}

	return path
}
