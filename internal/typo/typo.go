// Package typo — русская экранная типографика для HTML, который выдаёт
// markdown-рендерер: «ёлочки» с вложенными „лапками“, тире вместо дефиса,
// многоточие и неразрывные пробелы после коротких слов и чисел.
//
// Обрабатывается только текст между тегами: сами теги, атрибуты,
// HTML-комментарии и содержимое <pre>, <code>, <script> и т. п. проходят
// без изменений. Кавычки считаются только прямые ("), одиночные (')
// не трогаются — в русском тексте это апострофы (д'Артаньян).
package typo

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const nbsp = '\u00a0'

// skipTags — теги, внутри которых текст не трогаем.
var skipTags = map[string]bool{
	"pre": true, "code": true, "kbd": true, "samp": true, "var": true,
	"script": true, "style": true, "textarea": true,
}

// blockTags — теги, на которых кончается абзац: незакрытые кавычки и
// «предыдущий символ» дальше не переносятся.
var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "ul": true, "ol": true, "dl": true,
	"dt": true, "dd": true, "blockquote": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "table": true, "tr": true,
	"td": true, "th": true, "thead": true, "tbody": true, "hr": true,
	"figure": true, "figcaption": true, "section": true, "article": true,
	"details": true, "summary": true, "pre": true,
}

// inlineTags — строчные теги: текст до такого открывающего тега
// продолжается внутри него («в <a>Сеуле</a>»).
var inlineTags = map[string]bool{
	"a": true, "em": true, "strong": true, "b": true, "i": true, "u": true,
	"s": true, "del": true, "ins": true, "mark": true, "span": true,
	"abbr": true, "sup": true, "sub": true, "small": true, "q": true,
	"cite": true, "code": true, "kbd": true,
}

var voidTags = map[string]bool{
	"br": true, "img": true, "hr": true, "input": true, "meta": true,
	"link": true, "source": true, "wbr": true, "area": true, "col": true,
	"embed": true, "track": true,
}

// HTML применяет типографику к тексту в html.
func HTML(html string) string {
	var b strings.Builder
	b.Grow(len(html) + len(html)/16)
	st := newState()
	skip := 0
	for i := 0; i < len(html); {
		if isTagStart(html, i) {
			j := tagEnd(html, i)
			tag := html[i:j]
			name, closing := tagName(tag)
			if skipTags[name] && !voidTags[name] {
				if closing {
					if skip > 0 {
						skip--
					}
				} else if !strings.HasSuffix(tag, "/>") {
					skip++
				}
			}
			switch {
			case blockTags[name]:
				st = newState()
			case name == "br":
				st.boundary(' ')
			}
			b.WriteString(tag)
			i = j
			continue
		}
		j := i + 1
		for j < len(html) && !isTagStart(html, j) {
			j++
		}
		text := html[i:j]
		if skip > 0 {
			b.WriteString(text)
			if r, _ := utf8.DecodeLastRuneInString(text); r != utf8.RuneError {
				st.prev = r
			}
			st.word = -1
		} else {
			nextInline := false
			if j < len(html) {
				name, closing := tagName(html[j:tagEnd(html, j)])
				nextInline = !closing && inlineTags[name]
			}
			b.WriteString(st.text(text, nextInline))
		}
		i = j
	}
	return b.String()
}

// isTagStart: '<', за которым идёт имя тега, '/' или '!'. Одиночный '<'
// в сыром HTML («a < b») остаётся текстом.
func isTagStart(s string, i int) bool {
	if s[i] != '<' || i+1 >= len(s) {
		return false
	}
	c := s[i+1]
	return c == '/' || c == '!' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// tagEnd возвращает индекс сразу за концом тега (или комментария),
// начинающегося в s[i]. Кавычки в атрибутах учитываются.
func tagEnd(s string, i int) int {
	if strings.HasPrefix(s[i:], "<!--") {
		if k := strings.Index(s[i+4:], "-->"); k >= 0 {
			return i + 4 + k + 3
		}
		return len(s)
	}
	var quote byte
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		}
	}
	return len(s)
}

func tagName(tag string) (name string, closing bool) {
	t := strings.TrimPrefix(tag, "<")
	if strings.HasPrefix(t, "/") {
		closing = true
		t = t[1:]
	}
	end := 0
	for end < len(t) && (t[end] >= 'a' && t[end] <= 'z' || t[end] >= 'A' && t[end] <= 'Z' || t[end] >= '0' && t[end] <= '9') {
		end++
	}
	return strings.ToLower(t[:end]), closing
}

// item — символ текста. Сущность (&amp; и т. п.) — один item: raw хранит
// её как есть, r — символ, которым она считается в правилах.
type item struct {
	r   rune
	raw string
}

func (it item) String() string {
	if it.raw != "" {
		return it.raw
	}
	return string(it.r)
}

func parseItems(text string) []item {
	items := make([]item, 0, len(text))
	for i := 0; i < len(text); {
		if text[i] == '&' {
			if k := strings.IndexByte(text[i:], ';'); k > 1 && k < 12 {
				ent := text[i : i+k+1]
				i += k + 1
				switch ent {
				case "&quot;", "&#34;", "&#x22;":
					items = append(items, item{r: '"'})
				case "&nbsp;", "&#160;", "&#xa0;", "&#xA0;":
					items = append(items, item{r: nbsp, raw: ent})
				case "&amp;":
					items = append(items, item{r: '&', raw: ent})
				default:
					items = append(items, item{r: '?', raw: ent})
				}
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		items = append(items, item{r: r})
		i += size
	}
	return items
}

// state — то, что переносится между кусками текста одного абзаца,
// разделёнными строчными тегами.
type state struct {
	prev  rune // последний выведенный символ; ' ' — начало абзаца
	depth int  // глубина вложенности открытых кавычек
	// word — длина слова в конце выведенного текста, если слово началось
	// после пробела или открывающей скобки/кавычки; -1 — не считаем.
	word      int
	wordAlpha bool // слово из букв
	wordNum   bool // слово из цифр
}

func newState() state { return state{prev: ' '} }

func (st *state) boundary(r rune) {
	st.prev = r
	st.word, st.wordAlpha, st.wordNum = 0, false, false
}

func (st *state) track(r rune) {
	st.prev = r
	switch {
	case isSpace(r) || r == '(' || r == '[' || r == '«' || r == '„' || r == '—':
		st.word, st.wordAlpha, st.wordNum = 0, false, false
	case st.word < 0:
	case unicode.IsLetter(r):
		if st.wordNum {
			st.word = -1
		} else {
			st.word++
			st.wordAlpha = true
		}
	case unicode.IsDigit(r):
		if st.wordAlpha {
			st.word = -1
		} else {
			st.word++
			st.wordNum = true
		}
	default:
		st.word = -1
	}
}

// glue: стоит ли заменить пробел после текущего слова на неразрывный,
// если за пробелом идёт next.
func (st *state) glue(next rune) bool {
	if st.word <= 0 {
		return false
	}
	switch {
	case st.wordAlpha && st.word <= 2:
		return unicode.IsLetter(next) || unicode.IsDigit(next) || next == '"' || next == '«'
	case st.wordNum:
		return unicode.IsLetter(next)
	}
	return false
}

func isSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == nbsp }

// opensWord: после r может начинаться цитата или слово.
func opensWord(r rune) bool {
	return isSpace(r) || strings.ContainsRune("([«„—–-/", r)
}

// text обрабатывает кусок текста между тегами. nextInline — за куском идёт
// открывающий строчный тег, т. е. текст продолжается.
func (st *state) text(text string, nextInline bool) string {
	in := parseItems(text)
	out := make([]item, 0, len(in))
	after := ' ' // что считаем следующим символом за концом куска
	if nextInline {
		after = 'a'
	}
	peek := func(k int) rune {
		if k < len(in) {
			return in[k].r
		}
		return after
	}
	// lastSpace: последний выведенный символ — обычный пробел (его можно
	// сделать неразрывным).
	lastSpace := func() bool {
		return len(out) > 0 && out[len(out)-1].raw == "" && (out[len(out)-1].r == ' ' || out[len(out)-1].r == '\n')
	}
	emit := func(it item) {
		out = append(out, it)
		st.track(it.r)
	}

	for k := 0; k < len(in); k++ {
		it := in[k]
		switch it.r {
		case '.':
			// "..." → "…" (но не "....")
			if st.prev != '.' && peek(k+1) == '.' && peek(k+2) == '.' && peek(k+3) != '.' {
				emit(item{r: '…'})
				k += 2
				continue
			}
		case '-':
			// " - ", " -- ", " --- " → неразрывный пробел + "— "
			m := 1
			for m < 3 && peek(k+m) == '-' {
				m++
			}
			if isSpace(st.prev) && isSpace(peek(k+m)) && k+m < len(in) {
				if lastSpace() {
					out[len(out)-1] = item{r: nbsp}
				}
				emit(item{r: '—'})
				k += m - 1
				continue
			}
		case '—':
			if lastSpace() {
				out[len(out)-1] = item{r: nbsp}
			}
		case '«':
			st.depth++
		case '»':
			if st.depth > 0 {
				st.depth--
			}
		case '"':
			next := peek(k + 1)
			switch {
			case opensWord(st.prev) && !isSpace(next):
				if st.depth == 0 {
					it = item{r: '«'}
				} else {
					it = item{r: '„'}
				}
				st.depth++
			case !isSpace(st.prev) && st.depth > 0:
				if st.depth > 1 {
					it = item{r: '“'}
				} else {
					it = item{r: '»'}
				}
				st.depth--
			}
			// иначе (дюймы 27", кавычка между пробелами) — оставляем как есть
		case ' ', '\n':
			if st.glue(peek(k + 1)) {
				it = item{r: nbsp}
			}
		}
		emit(it)
	}

	var b strings.Builder
	b.Grow(len(text) + 8)
	for _, it := range out {
		b.WriteString(it.String())
	}
	return b.String()
}
