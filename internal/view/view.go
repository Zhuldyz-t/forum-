// Package view builds the shared html/template set, so the server and the
// tests parse templates with the same helper functions.
package view

import (
	"errors"
	"html/template"
)

// Funcs are the helper functions available in every template.
var Funcs = template.FuncMap{
	// dict builds a map from key/value pairs, to pass several values into
	// {{template}}: {{template "post-card" dict "Post" . "Page" $}}
	"dict": func(kv ...any) (map[string]any, error) {
		if len(kv)%2 != 0 {
			return nil, errors.New("dict: odd number of arguments")
		}
		m := make(map[string]any, len(kv)/2)
		for i := 0; i < len(kv); i += 2 {
			key, ok := kv[i].(string)
			if !ok {
				return nil, errors.New("dict: keys must be strings")
			}
			m[key] = kv[i+1]
		}
		return m, nil
	},
}

// ParseGlob parses all templates matching pattern with Funcs available.
func ParseGlob(pattern string) (*template.Template, error) {
	return template.New("").Funcs(Funcs).ParseGlob(pattern)
}
