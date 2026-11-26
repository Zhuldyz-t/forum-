// Package markdown renders the small Markdown subset used in posts and
// comments. It is safe by construction: every piece of user text is
// HTML-escaped first, and the renderer only ever adds its own fixed tags, so
// raw HTML in the input is shown as text. Link targets are checked against
// an allowlist of schemes.
package markdown

import (
	"html/template"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Options controls what a render may produce.
type Options struct {
	// Headings allows "# Title" lines (posts only); otherwise they stay text.
	Headings bool
	// Mentions maps a lowercased username to the stored username of an
	// existing user; only those @mentions become profile links.
	Mentions map[string]string
}

var (
	fenceRe    = regexp.MustCompile("^\\s*```\\s*([A-Za-z0-9_+#.-]{0,20})\\s*$")
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	quoteRe    = regexp.MustCompile(`^\s{0,3}>\s?(.*)$`)
	ulRe       = regexp.MustCompile(`^\s{0,3}[-*+]\s+(.*)$`)
	olRe       = regexp.MustCompile(`^\s{0,3}\d{1,9}[.)]\s+(.*)$`)
	hrRe       = regexp.MustCompile(`^\s{0,3}(?:-\s*){3,}$|^\s{0,3}(?:\*\s*){3,}$|^\s{0,3}(?:_\s*){3,}$`)
	codeSpanRe = regexp.MustCompile("`([^`\n]+)`")
	linkRe     = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	autoLinkRe = regexp.MustCompile(`https?://[^\s<>()\x{E000}\x{E001}]*[^\s<>().,;:!?'"\x{E000}\x{E001}]`)
	mentionRe  = regexp.MustCompile(`(^|[^\w@/])@([A-Za-z0-9_]{3,20})\b`)
	boldRe     = regexp.MustCompile(`\*\*([^*\n]+?)\*\*|__([^_\n]+?)__`)
	italicRe   = regexp.MustCompile(`(^|[^\w*])\*([^*\n]+?)\*`)
	italicURe  = regexp.MustCompile(`(^|[^\w_])_([^_\n]+?)_($|[^\w_])`)
	tokenRe    = regexp.MustCompile(tokOpen + `(\d+)` + tokClose)
	langRe     = regexp.MustCompile(`^[a-z0-9_+#.-]{1,20}$`)
)

// Placeholder delimiters for already-rendered fragments. They are Unicode
// private-use characters: HTMLEscapeString leaves them alone (unlike NUL,
// which it rewrites), and they are stripped from user input.
const (
	tokOpen  = "\uE000"
	tokClose = "\uE001"
)

// Render converts Markdown to safe HTML.
func Render(src string, o Options) template.HTML {
	r := &renderer{opts: o}
	return template.HTML(r.blocks(normalize(src)))
}

func normalize(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.NewReplacer(tokOpen, "", tokClose, "", "\x00", "").Replace(s) // reserved for placeholders
	return strings.Split(s, "\n")
}

type renderer struct{ opts Options }

func esc(s string) string { return template.HTMLEscapeString(s) }

func (r *renderer) blocks(lines []string) string {
	var out strings.Builder
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		parts := make([]string, len(para))
		for i, l := range para {
			parts[i] = r.inline(strings.TrimSpace(l))
		}
		out.WriteString("<p>" + strings.Join(parts, "<br>") + "</p>")
		para = nil
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case fenceRe.MatchString(line):
			flush()
			lang := strings.ToLower(fenceRe.FindStringSubmatch(line)[1])
			var code []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "```"; i++ {
				code = append(code, lines[i])
			}
			out.WriteString(codeBlock(strings.Join(code, "\n"), lang))
		case strings.TrimSpace(line) == "":
			flush()
		case r.opts.Headings && headingRe.MatchString(line):
			flush()
			m := headingRe.FindStringSubmatch(line)
			// The post title is the page's h1, so # maps to h2 … h4.
			level := min(len(m[1])+1, 4)
			tag := "h" + strconv.Itoa(level)
			out.WriteString("<" + tag + ">" + r.inline(m[2]) + "</" + tag + ">")
		case hrRe.MatchString(line):
			flush()
			out.WriteString("<hr>")
		case quoteRe.MatchString(line):
			flush()
			var inner []string
			for ; i < len(lines) && quoteRe.MatchString(lines[i]); i++ {
				inner = append(inner, quoteRe.FindStringSubmatch(lines[i])[1])
			}
			i--
			sub := &renderer{opts: Options{Mentions: r.opts.Mentions}}
			out.WriteString("<blockquote>" + sub.blocks(inner) + "</blockquote>")
		case ulRe.MatchString(line), olRe.MatchString(line):
			flush()
			re, tag := ulRe, "ul"
			if !ulRe.MatchString(line) {
				re, tag = olRe, "ol"
			}
			out.WriteString("<" + tag + ">")
			for ; i < len(lines) && re.MatchString(lines[i]); i++ {
				out.WriteString("<li>" + r.inline(re.FindStringSubmatch(lines[i])[1]) + "</li>")
			}
			i--
			out.WriteString("</" + tag + ">")
		default:
			para = append(para, line)
		}
	}
	flush()
	return out.String()
}

func codeBlock(code, lang string) string {
	if !langRe.MatchString(lang) {
		lang = ""
	}
	label := lang
	if label == "" {
		label = "код"
	}
	var b strings.Builder
	b.WriteString(`<div class="code-block"><div class="code-head"><span class="code-lang">` + esc(label) + `</span>`)
	b.WriteString(`<button type="button" class="code-copy" data-code-copy>Копировать</button></div>`)
	b.WriteString(`<pre><code`)
	if lang != "" {
		b.WriteString(` data-lang="` + esc(lang) + `"`)
	}
	b.WriteString(`>` + esc(code) + `</code></pre></div>`)
	return b.String()
}

// inline renders one line: code spans are taken out first so nothing inside
// them is formatted.
func (r *renderer) inline(s string) string {
	var out strings.Builder
	last := 0
	for _, m := range codeSpanRe.FindAllStringSubmatchIndex(s, -1) {
		out.WriteString(r.text(s[last:m[0]]))
		out.WriteString("<code>" + esc(s[m[2]:m[3]]) + "</code>")
		last = m[1]
	}
	out.WriteString(r.text(s[last:]))
	return out.String()
}

// text formats plain text: links, autolinks and mentions are swapped for
// placeholders, the rest is escaped, emphasis is applied, then the
// placeholders are put back.
func (r *renderer) text(s string) string {
	var tokens []string
	put := func(html string) string {
		tokens = append(tokens, html)
		return tokOpen + strconv.Itoa(len(tokens)-1) + tokClose
	}

	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		href, ok := SafeURL(sub[2])
		if !ok {
			return m
		}
		return put(anchor(href, emphasis(esc(sub[1]))))
	})
	s = autoLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		href, ok := SafeURL(m)
		if !ok {
			return m
		}
		return put(anchor(href, esc(m)))
	})
	if r.opts.Mentions != nil {
		s = mentionRe.ReplaceAllStringFunc(s, func(m string) string {
			sub := mentionRe.FindStringSubmatch(m)
			name, ok := r.opts.Mentions[strings.ToLower(sub[2])]
			if !ok {
				return m
			}
			return sub[1] + put(`<a href="/user/`+url.PathEscape(name)+`" class="mention">@`+esc(name)+`</a>`)
		})
	}

	s = emphasis(esc(s))
	return tokenRe.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(tokenRe.FindStringSubmatch(m)[1])
		if n < len(tokens) {
			return tokens[n]
		}
		return ""
	})
}

func emphasis(s string) string {
	s = boldRe.ReplaceAllString(s, "<strong>$1$2</strong>")
	s = italicRe.ReplaceAllString(s, "$1<em>$2</em>")
	s = italicURe.ReplaceAllString(s, "$1<em>$2</em>$3")
	return s
}

func anchor(href, inner string) string {
	if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "#") {
		return `<a href="` + esc(href) + `">` + inner + `</a>`
	}
	return `<a href="` + esc(href) + `" target="_blank" rel="noopener noreferrer">` + inner + `</a>`
}

// SafeURL accepts http(s) and mailto links, site-relative paths and
// fragments; everything else (javascript:, data:, //host …) is rejected.
func SafeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\x00\n\r\t\\") {
		return "", false
	}
	if strings.HasPrefix(raw, "#") || (strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//")) {
		return raw, true
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", false
		}
		return u.String(), true
	case "mailto":
		return u.String(), true
	}
	return "", false
}

var (
	plainFenceRe = regexp.MustCompile("(?m)^\\s*```.*$")
	plainHeadRe  = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	plainQuoteRe = regexp.MustCompile(`(?m)^\s{0,3}>\s?`)
	plainListRe  = regexp.MustCompile(`(?m)^\s{0,3}(?:[-*+]|\d{1,9}[.)])\s+`)
	plainEmphRe  = regexp.MustCompile(`\*\*|__|\*|` + "`")
)

// PlainText strips Markdown syntax for previews and search highlighting.
func PlainText(src string) string {
	s := strings.ReplaceAll(src, "\r\n", "\n")
	s = plainFenceRe.ReplaceAllString(s, "")
	s = linkRe.ReplaceAllString(s, "$1")
	s = plainHeadRe.ReplaceAllString(s, "")
	s = plainQuoteRe.ReplaceAllString(s, "")
	s = plainListRe.ReplaceAllString(s, "• ")
	s = plainEmphRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// Mentions returns the lowercased usernames mentioned in src, outside code.
func Mentions(src string) []string {
	var text []string
	inFence := false
	for _, line := range normalize(src) {
		if fenceRe.MatchString(line) || (inFence && strings.TrimSpace(line) == "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			text = append(text, codeSpanRe.ReplaceAllString(line, ""))
		}
	}
	seen := map[string]bool{}
	var names []string
	for _, m := range mentionRe.FindAllStringSubmatch(strings.Join(text, "\n"), -1) {
		n := strings.ToLower(m[2])
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	return names
}
