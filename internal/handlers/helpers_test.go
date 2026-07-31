package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ───── slugify / transliterate ─────

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello World", "hello-world"},
		{"  spaces  ", "spaces"},
		{"multiple---dashes", "multiple-dashes"},
		{"Привет Мир", "privet-mir"},
		{"Ёжик в тумане", "yozhik-v-tumane"},
		{"go1.22 release", "go122-release"},
		{"__underscores__", "underscores"},
		{"щука", "shchuka"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTransliterate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"привет", "privet"},
		{"щука", "shchuka"},
		{"ёжик", "yozhik"},
		{"твёрдый знак ъ", "tvyordyj znak "},
		{"мягкий знак ь", "myagkij znak "},
		{"LATIN", "LATIN"}, // uppercase Latin passes through unchanged
	}
	for _, tc := range cases {
		if got := transliterate(tc.in); got != tc.want {
			t.Errorf("transliterate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ───── excerpt ─────

func TestExcerpt(t *testing.T) {
	got := excerpt("<p>Hello <strong>world</strong></p>")
	if got != "Hello world" {
		t.Errorf("excerpt HTML strip: got %q", got)
	}

	got = excerpt("<p>Короткий текст</p>")
	if got != "Короткий текст" {
		t.Errorf("excerpt short: got %q", got)
	}

	// 50 Russian words × ~6 chars ≈ 300 runes — should be truncated
	var long string
	for i := 0; i < 50; i++ {
		long += "слово "
	}
	got = excerpt("<p>" + long + "</p>")
	runes := []rune(got)
	if len(runes) > 284 { // 280 runes + "…" (1 rune) + possible space before cut
		t.Errorf("excerpt long: too long, got %d runes", len(runes))
	}
	last := string(runes[len(runes)-1:])
	if last != "…" {
		t.Errorf("excerpt long: should end with …, got %q", last)
	}
}

// ───── dayWord / weekWord ─────

func TestDayWord(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "день"}, {21, "день"}, {31, "день"},
		{2, "дня"}, {3, "дня"}, {4, "дня"}, {22, "дня"},
		{5, "дней"}, {11, "дней"}, {12, "дней"}, {15, "дней"},
	}
	for _, tc := range cases {
		if got := dayWord(tc.n); got != tc.want {
			t.Errorf("dayWord(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestWeekWord(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "неделю"}, {21, "неделю"},
		{2, "недели"}, {3, "недели"}, {4, "недели"},
		{5, "недель"}, {11, "недель"},
	}
	for _, tc := range cases {
		if got := weekWord(tc.n); got != tc.want {
			t.Errorf("weekWord(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// ───── relDate ─────

func TestRelDate(t *testing.T) {
	now := time.Now()

	if got := relDate(now.Add(-1 * time.Hour)); got != "сегодня" {
		t.Errorf("relDate(today): got %q", got)
	}
	if got := relDate(now.Add(-25 * time.Hour)); got != "вчера" {
		t.Errorf("relDate(yesterday): got %q", got)
	}
	if got := relDate(now.Add(-3 * 24 * time.Hour)); got != "3 дня назад" {
		t.Errorf("relDate(3 days): got %q", got)
	}
	if got := relDate(now.Add(-14 * 24 * time.Hour)); got != "2 недели назад" {
		t.Errorf("relDate(2 weeks): got %q", got)
	}

	old := time.Date(2020, 6, 15, 0, 0, 0, 0, time.UTC)
	if got := relDate(old); got != "15 Jun 2020" {
		t.Errorf("relDate(old): got %q", got)
	}
}

// ───── isAllowedImageExt ─────

func TestIsAllowedImageExt(t *testing.T) {
	allowed := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".JPG", ".JPEG", ".PNG"}
	for _, ext := range allowed {
		if !isAllowedImageExt(ext) {
			t.Errorf("isAllowedImageExt(%q): want true", ext)
		}
	}
	denied := []string{".php", ".exe", ".svg", ".html", ""}
	for _, ext := range denied {
		if isAllowedImageExt(ext) {
			t.Errorf("isAllowedImageExt(%q): want false", ext)
		}
	}
}

// ───── isAllowedImageContent ─────

func TestIsAllowedImageContent(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 'J', 'F', 'I', 'F', 0}
	if !isAllowedImageContent(jpeg) {
		t.Error("JPEG should be allowed")
	}

	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !isAllowedImageContent(png) {
		t.Error("PNG should be allowed")
	}

	// WebP RIFF magic
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), make([]byte, 20)...)
	if !isAllowedImageContent(webp) {
		t.Error("WebP should be allowed")
	}

	if isAllowedImageContent([]byte("this is not an image at all!!!!!")) {
		t.Error("random bytes should be denied")
	}
}

// ───── clientIP ─────

func TestClientIP(t *testing.T) {
	trustedProxy = false
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")

	if got := clientIP(r); got != "1.2.3.4" {
		t.Errorf("no-trust: got %q, want 1.2.3.4", got)
	}

	trustedProxy = true
	if got := clientIP(r); got != "9.9.9.9" {
		t.Errorf("trusted: got %q, want 9.9.9.9", got)
	}

	// Multiple IPs in X-Forwarded-For — take the last one (appended by our
	// trusted proxy); earlier entries are client-supplied and spoofable.
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 1.1.1.1")
	if got := clientIP(r); got != "1.1.1.1" {
		t.Errorf("multi XFF: got %q, want 1.1.1.1", got)
	}

	trustedProxy = false
}

// ───── Login rate limiter ─────

func resetRateLimiter() {
	loginMu.Lock()
	loginAttempts = map[string]*loginAttempt{}
	loginMu.Unlock()
}

func fakeRequest(ip string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	r.RemoteAddr = ip + ":1234"
	return r
}

func TestLoginRateLimiterLockout(t *testing.T) {
	resetRateLimiter()
	r := fakeRequest("10.0.0.1")

	if !LoginAllowed(r) {
		t.Fatal("should be allowed before any failures")
	}
	for i := 0; i < 4; i++ {
		RecordLoginFailure(r)
	}
	if !LoginAllowed(r) {
		t.Error("should still be allowed after 4 failures")
	}
	RecordLoginFailure(r) // 5th — triggers lockout
	if LoginAllowed(r) {
		t.Error("should be locked out after 5 failures")
	}
}

func TestLoginRateLimiterSuccessClearsLockout(t *testing.T) {
	resetRateLimiter()
	r := fakeRequest("10.0.0.2")

	for i := 0; i < 5; i++ {
		RecordLoginFailure(r)
	}
	if LoginAllowed(r) {
		t.Fatal("should be locked out")
	}
	RecordLoginSuccess(r)
	if !LoginAllowed(r) {
		t.Error("should be allowed after successful login clears lockout")
	}
}

func TestLoginRateLimiterIndependentIPs(t *testing.T) {
	resetRateLimiter()
	r1 := fakeRequest("10.0.1.1")
	r2 := fakeRequest("10.0.1.2")

	for i := 0; i < 5; i++ {
		RecordLoginFailure(r1)
	}
	if LoginAllowed(r1) {
		t.Error("r1 should be locked out")
	}
	if !LoginAllowed(r2) {
		t.Error("r2 should not be affected by r1 lockout")
	}
}
