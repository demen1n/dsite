package typo

import (
	"strings"
	"testing"
)

// n — неразрывный пробел в ожидаемых строках (так читаемее, чем \u00a0).
func n(s string) string { return strings.ReplaceAll(s, "~", "\u00a0") }

func TestHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"quotes", `<p>Мы читали "Ревизора".</p>`, n(`<p>Мы~читали «Ревизора».</p>`)},
		{"quot entity", `<p>Читали &quot;Ревизора&quot;.</p>`, `<p>Читали «Ревизора».</p>`},
		{"nested quotes", `<p>"Книга "Мастер" хороша"</p>`, `<p>«Книга „Мастер“ хороша»</p>`},
		{"straight inside typed guillemets", `<p>«Фильм "Брат"»</p>`, `<p>«Фильм „Брат“»</p>`},
		{"quotes around inline tag", `<p>Читаю "<em>Войну</em>" снова</p>`, `<p>Читаю «<em>Войну</em>» снова</p>`},
		{"quotes reset per paragraph", `<p>"оборвано</p><p>"новая"</p>`, `<p>«оборвано</p><p>«новая»</p>`},
		{"inches stay", `<p>Монитор 27" хорош</p>`, `<p>Монитор 27" хорош</p>`},
		{"lone quote stays", `<p>знак " отдельно</p>`, `<p>знак " отдельно</p>`},
		{"apostrophe stays", `<p>д'Артаньян</p>`, `<p>д'Артаньян</p>`},
		{"dash", `<p>Москва - столица</p>`, n(`<p>Москва~— столица</p>`)},
		{"double hyphen dash", `<p>Москва -- столица</p>`, n(`<p>Москва~— столица</p>`)},
		{"typed dash", `<p>Москва — столица</p>`, n(`<p>Москва~— столица</p>`)},
		{"hyphen in word stays", `<p>из-за кое-где 2026-10-16</p>`, `<p>из-за кое-где 2026-10-16</p>`},
		{"ellipsis", `<p>Ну...</p>`, `<p>Ну…</p>`},
		{"four dots stay", `<p>Ну....</p>`, `<p>Ну....</p>`},
		{"short words", `<p>Я иду в лес и на море</p>`, n(`<p>Я~иду в~лес и~на~море</p>`)},
		{"number and word", `<p>Прошёл 15 км за 3 часа</p>`, n(`<p>Прошёл 15~км~за~3~часа</p>`)},
		{"no glue after punctuation", `<p>т.е. так</p>`, `<p>т.е. так</p>`},
		{"glue before inline tag", `<p>в <a href="/x">Сеуле</a></p>`, n(`<p>в~<a href="/x">Сеуле</a></p>`)},
		{"glue before quote", `<p>в "Пятёрочку"</p>`, n(`<p>в~«Пятёрочку»</p>`)},
		{"code untouched", `<p>Флаг <code>"a" - b...</code> и "c"</p>`, n(`<p>Флаг <code>"a" - b...</code> и~«c»</p>`)},
		{"pre untouched", `<pre><code>x = "a" - "b"
в лес</code></pre>`, `<pre><code>x = "a" - "b"
в лес</code></pre>`},
		{"attributes untouched", `<p><a href="/a - b" title="x &quot;y&quot;">ссылка</a></p>`, `<p><a href="/a - b" title="x &quot;y&quot;">ссылка</a></p>`},
		{"comment untouched", `<p>а</p><!--more "x" - y--><p>б</p>`, `<p>а</p><!--more "x" - y--><p>б</p>`},
		{"other entities kept", `<p>A &amp; B &lt;tag&gt;</p>`, `<p>A &amp; B &lt;tag&gt;</p>`},
		{"lone less-than is text", `<p>a < b - c</p>`, n(`<p>a < b~— c</p>`)},
		{"soft line break glued", "<p>в\nлес</p>", n("<p>в~лес</p>")},
		{"br resets word", `<p>abc<br>в лес</p>`, n(`<p>abc<br>в~лес</p>`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTML(tc.in); got != tc.want {
				t.Errorf("HTML(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHTMLIdempotent(t *testing.T) {
	in := `<p>"Книга "Мастер"" - это в 2 томах...</p>`
	once := HTML(in)
	if twice := HTML(once); twice != once {
		t.Errorf("not idempotent:\n once %q\ntwice %q", once, twice)
	}
}
