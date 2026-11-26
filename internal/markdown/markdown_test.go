package markdown

import (
	"regexp"
	"strings"
	"testing"
)

func render(src string, o Options) string { return string(Render(src, o)) }

func TestFormatting(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**жирный** и *курсив*", "<p><strong>жирный</strong> и <em>курсив</em></p>"},
		{"_курсив_ тоже", "<p><em>курсив</em> тоже</p>"},
		{"код `a < b`", "<p>код <code>a &lt; b</code></p>"},
		{"`**не жирный**`", "<p><code>**не жирный**</code></p>"},
		{"- раз\n- два", "<ul><li>раз</li><li>два</li></ul>"},
		{"1. раз\n2. два", "<ol><li>раз</li><li>два</li></ol>"},
		{"> цитата\n> ещё", "<blockquote><p>цитата<br>ещё</p></blockquote>"},
		{"строка 1\nстрока 2\n\nабзац", "<p>строка 1<br>строка 2</p><p>абзац</p>"},
		{"[сайт](https://example.com/a?b=1&c=2)", `<p><a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer">сайт</a></p>`},
		{"см. https://go.dev.", `<p>см. <a href="https://go.dev" target="_blank" rel="noopener noreferrer">https://go.dev</a>.</p>`},
		{"snake_case_name", "<p>snake_case_name</p>"},
	}
	for _, c := range cases {
		if got := render(c.in, Options{}); got != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestHeadingsOnlyWhenAllowed(t *testing.T) {
	if got := render("# Заголовок", Options{Headings: true}); got != "<h2>Заголовок</h2>" {
		t.Errorf("post heading: %s", got)
	}
	if got := render("# Заголовок", Options{}); got != "<p># Заголовок</p>" {
		t.Errorf("comment heading must stay text: %s", got)
	}
}

func TestCodeBlock(t *testing.T) {
	got := render("```go\nfmt.Println(\"<b>\")\n```", Options{})
	for _, want := range []string{`data-lang="go"`, `fmt.Println(&#34;&lt;b&gt;&#34;)`, `class="code-lang">go<`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	// A closing tag inside the block must not escape it.
	got = render("```html\n</code></pre><script>alert(1)</script>\n```", Options{})
	if strings.Contains(got, "<script>") {
		t.Errorf("script leaked out of code block: %s", got)
	}
}

var (
	tagRe  = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)([^>]*)>`)
	attrRe = regexp.MustCompile(`\s([a-zA-Z-]+)="([^"]*)"`)
)

// allowed tags and, per tag, allowed attributes.
var allowed = map[string]map[string]bool{
	"p": {}, "br": {}, "strong": {}, "em": {}, "code": {"data-lang": true}, "pre": {},
	"ul": {}, "ol": {}, "li": {}, "blockquote": {}, "h2": {}, "h3": {}, "h4": {}, "hr": {},
	"a":      {"href": true, "target": true, "rel": true, "class": true},
	"div":    {"class": true},
	"span":   {"class": true},
	"button": {"type": true, "class": true, "data-code-copy": true},
}

// checkSafe fails if the output has any tag or attribute outside the
// allowlist, or a link to anything but http(s), mailto, / or #.
func checkSafe(t *testing.T, in, out string) {
	t.Helper()
	for _, m := range tagRe.FindAllStringSubmatch(out, -1) {
		attrs, ok := allowed[strings.ToLower(m[2])]
		if !ok {
			t.Errorf("%q: tag <%s> not allowed in %s", in, m[2], out)
			continue
		}
		rest := attrRe.ReplaceAllString(m[3], "")
		rest = strings.ReplaceAll(rest, " data-code-copy", "")
		if strings.TrimSpace(rest) != "" {
			t.Errorf("%q: unexpected attribute text %q in %s", in, rest, out)
		}
		for _, a := range attrRe.FindAllStringSubmatch(m[3], -1) {
			if !attrs[a[1]] {
				t.Errorf("%q: attribute %s on <%s> not allowed", in, a[1], m[2])
			}
			if a[1] == "href" {
				h := strings.ToLower(a[2])
				if !(strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "http://") ||
					strings.HasPrefix(h, "mailto:") || strings.HasPrefix(h, "#") ||
					(strings.HasPrefix(h, "/") && !strings.HasPrefix(h, "//"))) {
					t.Errorf("%q: unsafe href %q", in, a[2])
				}
			}
		}
	}
}

func TestXSS(t *testing.T) {
	bad := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`[клик](javascript:alert(1))`,
		`[клик](JaVaScRiPt:alert(1))`,
		`[клик](data:text/html;base64,PHNjcmlwdD4=)`,
		`[клик](//evil.example)`,
		`[x](javascript&#58;alert(1))`,
		`[x" onmouseover="alert(1)](https://example.com)`,
		`[x](https://example.com/" onmouseover="alert(1))`,
		`**<svg onload=alert(1)>**`,
		"`<script>`",
		`> <iframe src="javascript:alert(1)">`,
		`- <a href="javascript:alert(1)">x</a>`,
		"```\"><script>alert(1)</script>\n```",
		"https://example.com/<script>",
		"@admin<script>",
		"\uE0000\uE001 [a](https://x.y)",
	}
	for _, in := range bad {
		out := render(in, Options{Headings: true, Mentions: map[string]string{"admin": "admin"}})
		checkSafe(t, in, out)
	}
}

func TestMentions(t *testing.T) {
	src := "привет @Rubin и @nobody, почта a@b.com\n`@incode`\n```\n@infence\n```"
	if got := strings.Join(Mentions(src), ","); got != "rubin,nobody" {
		t.Errorf("Mentions = %q", got)
	}
	got := render("привет @rubin и @nobody", Options{Mentions: map[string]string{"rubin": "Rubin"}})
	want := `<p>привет <a href="/user/Rubin" class="mention">@Rubin</a> и @nobody</p>`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText("# Заголовок\n**жирный** [ссылка](https://x.y)\n- пункт\n```go\nx := 1\n```")
	want := "Заголовок\nжирный ссылка\n• пункт\n\nx := 1"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
