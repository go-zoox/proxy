package rewriter

import "regexp"

// Rewriter is a rewrite rule.
type Rewriter struct {
	From string `yaml:"from" json:"from"`
	To   string `yaml:"to" json:"to"`
}

func (r *Rewriter) re() *regexp.Regexp {
	return regexp.MustCompile(r.From)
}

// IsMatch returns true if the path matches the rule.
func (r *Rewriter) IsMatch(path string) bool {
	return r.re().MatchString(path)
}

// Rewrite rewrites the path.
func (r *Rewriter) Rewrite(path string) string {
	return r.re().ReplaceAllString(path, r.To)
}

// Rewriters is a list of rewrite rules.
type Rewriters []Rewriter

// Rewrite rewrites the path.
func (r *Rewriters) Rewrite(path string) string {
	for _, rewriter := range *r {
		if rewriter.IsMatch(path) {
			return rewriter.Rewrite(path)
		}
	}

	return path
}
