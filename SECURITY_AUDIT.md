# Security Audit — dsite

**Дата:** 2026-05-03
**Аудитор:** Claude (полный ручной аудит исходного кода)
**Объём:** 100 % Go-кода, шаблонов, Dockerfile, docker-compose.yml, GitHub Actions, конфигов
**Версия проекта:** main @ `804a6f1`

Документ предназначен для исполнителя, который будет устранять уязвимости.
Каждая находка содержит:
- **Уровень риска** (Critical / High / Medium / Low / Info).
- **Где** — точные файлы и строки.
- **Что** — суть проблемы.
- **Почему** — последствия эксплуатации.
- **Как чинить** — конкретный путь решения с примерами кода.

Резюме (TL;DR):
- 1 High, 4 Medium, 7 Low, 6 Info.
- Critical-уязвимостей не обнаружено: SQL-запросы параметризованы, пароли хешируются bcrypt, сессии криптостойкие, CSRF защищён через Origin/Referer + SameSite=Lax, заголовки безопасности базово настроены.
- Главные слабые места: публичный листинг `/uploads/`, отсутствие таймаутов HTTP-сервера, Markdown с `WithUnsafe()` + CSP `'unsafe-inline'` (XSS-риск при компрометации админки), отсутствие лимита размера запроса для multipart-загрузок.

---

## Оглавление по уровню риска

### High
- [H-1. Публичный directory listing `/uploads/` (Information Disclosure)](#h-1-публичный-directory-listing-uploads)

### Medium
- [M-1. Нет таймаутов HTTP-сервера → Slowloris / DoS](#m-1-нет-таймаутов-http-сервера-slowloris--dos)
- [M-2. Нет глобального лимита размера тела запроса (multipart-загрузки)](#m-2-нет-глобального-лимита-размера-тела-запроса-multipart-загрузки)
- [M-3. Markdown с `html.WithUnsafe()` + CSP `script-src 'unsafe-inline'` → stored XSS при компрометации админа](#m-3-markdown-с-htmlwithunsafe--csp-script-src-unsafe-inline-stored-xss-при-компрометации-админа)
- [M-4. Утечка временных multipart-файлов (`r.MultipartForm.RemoveAll()` не вызывается)](#m-4-утечка-временных-multipart-файлов-rmultipartformremoveall-не-вызывается)

### Low
- [L-1. Тайминговый оракул логина (нет dummy-bcrypt при отсутствии юзера)](#l-1-тайминговый-оракул-логина-нет-dummy-bcrypt-при-отсутствии-юзера)
- [L-2. `X-Forwarded-Proto` и `Host` доверяются без проверки proxy](#l-2-x-forwarded-proto-и-host-доверяются-без-проверки-proxy)
- [L-3. Утечка памяти rate-limiter'а: бесконечно растущая `loginAttempts`-карта](#l-3-утечка-памяти-rate-limiterа-бесконечно-растущая-loginattempts-карта)
- [L-4. Установка админа (`/admin/setup`) без какого-либо доказательства владения](#l-4-установка-админа-adminsetup-без-какого-либо-доказательства-владения)
- [L-5. `secureCookies` опционален — небезопасный дефолт](#l-5-securecookies-опционален-небезопасный-дефолт)
- [L-6. HSTS отдаётся даже по HTTP](#l-6-hsts-отдаётся-даже-по-http)
- [L-7. Нет защиты от content-injection в `LIKE` (SQL wildcard)](#l-7-нет-защиты-от-content-injection-в-like-sql-wildcard)

### Info / Hardening
- [I-1. CSP позволяет `unsafe-inline` для скриптов и стилей](#i-1-csp-позволяет-unsafe-inline-для-скриптов-и-стилей)
- [I-2. CSRF защищён только Origin/Referer (нет CSRF-токенов)](#i-2-csrf-защищён-только-originreferer-нет-csrf-токенов)
- [I-3. Нет endpoint'а смены пароля и инвалидации сессий](#i-3-нет-endpointа-смены-пароля-и-инвалидации-сессий)
- [I-4. Слабая `bcrypt.DefaultCost = 10`](#i-4-слабая-bcryptdefaultcost--10)
- [I-5. Нет лимита числа файлов в одной загрузке и числа постов на странице](#i-5-нет-лимита-числа-файлов-в-одной-загрузке-и-числа-постов-на-странице)
- [I-6. Утилита `cmd/recompress` запускает внешний `cwebp`](#i-6-утилита-cmdrecompress-запускает-внешний-cwebp)

---

## High

### H-1. Публичный directory listing `/uploads/`

- **Файл:** `cmd/dsite/main.go:47`
- **Триаж:** подтверждено эмпирически — `curl http://localhost:8080/uploads/` возвращает HTML со списком всех файлов.

#### Что
`http.FileServer(http.Dir(cfg.UploadsDir))` обслуживает `/uploads/` без `index.html`. Стандартное поведение `net/http` — отдавать листинг содержимого каталога. Любой неаутентифицированный пользователь может получить полный список:

```
$ curl http://localhost:8080/uploads/
<a href="1772344497_e86d1928e1e34344.webp">…</a>
<a href="1772344729_fe073231b4c8b5ed.webp">…</a>
…
```

#### Почему опасно
1. Раскрываются файлы, которые ещё не опубликованы (обложки черновиков, фотографии, удалённые из БД, но оставшиеся на диске).
2. Любые приватные документы, попавшие в `uploads/` в обход админки (через `cp`/`scp`), становятся индексируемыми.
3. SEO-боты индексируют листинг; деиндексация позже трудна.
4. Раскрывает временные метки и шаблон именования, упрощая брутфорс.

#### Как чинить
Обернуть `FileServer` в middleware, который для пути с `/` отвечает 404:

```go
// cmd/dsite/main.go
func noListing(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasSuffix(r.URL.Path, "/") {
            http.NotFound(w, r)
            return
        }
        next.ServeHTTP(w, r)
    })
}

mux.Handle("GET /uploads/", immutableCache(
    noListing(http.StripPrefix("/uploads/", http.FileServer(http.Dir(cfg.UploadsDir)))),
))
mux.Handle("GET /static/", noListing(
    http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))),
))
```

Альтернатива — пустой `index.html` в каталоге, но middleware проще и надёжнее.

---

## Medium

### M-1. Нет таймаутов HTTP-сервера → Slowloris / DoS

- **Файл:** `cmd/dsite/main.go:106` — `http.ListenAndServe(":"+cfg.Port, ...)`

#### Что
`http.ListenAndServe` использует дефолтные значения таймаутов (бесконечные). Атакующий может открыть сотни TCP-соединений и медленно слать заголовки (Slowloris) или медленно читать ответ — сервер держит горутины и память заняты, легитимные пользователи блокируются. Особенно критично для контейнера с `mem_limit: 256m` (docker-compose.yml).

#### Как чинить
Заменить `ListenAndServe` на явный `http.Server` с таймаутами:

```go
srv := &http.Server{
    Addr:              ":" + cfg.Port,
    Handler:           securityHeaders(csrfCheck(mux)),
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       60 * time.Second,   // multipart-аплоады могут идти долго
    WriteTimeout:      60 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 16,            // 64 KB
}
log.Printf("🚀 Server running at http://localhost:%s", cfg.Port)
log.Fatal(srv.ListenAndServe())
```

Если есть длинные загрузки — поднимай `ReadTimeout`/`WriteTimeout` именно для этих эндпоинтов через `http.MaxBytesReader` + ручной `r.Context().WithTimeout`. Но 60 с обычно достаточно.

---

### M-2. Нет глобального лимита размера тела запроса (multipart-загрузки)

- **Файлы:**
  - `internal/handlers/admin.go:117` (`CreatePost`)
  - `internal/handlers/admin.go:181` (`UpdatePost`)
  - `internal/handlers/admin.go:305` (`UploadPhoto`)
  - `internal/handlers/admin.go:519` (`UploadImage`)

#### Что
`r.ParseMultipartForm(32 << 20)` ограничивает только то, сколько остаётся в памяти — 32 MB. Всё, что больше, пишется во временные файлы на диск. Размер запроса целиком ничем не ограничен. Аутентифицированный админ может случайно (или злоумышленник, угнавший сессию) залить файл размером в десятки гигабайт и забить диск контейнера. Учитывая, что docker-compose ограничивает память 256 MB, parse-form будет складывать всё на диск.

В `PreviewMD` `MaxBytesReader` уже используется (строка 232) — нужно сделать то же для всех загрузочных эндпоинтов.

#### Как чинить
Перед `ParseMultipartForm` ограничить тело:

```go
// admin.go — для всех 4 хендлеров
const maxUploadSize = 50 << 20 // 50 MB на запрос
r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
if err := r.ParseMultipartForm(8 << 20); err != nil {
    http.Error(w, "файл слишком большой", http.StatusRequestEntityTooLarge)
    return
}
```

Memory-limit `8 << 20` достаточно — крупные файлы и так пойдут на диск, но ограниченные `MaxBytesReader`.

Дополнительно: в `UploadPhoto` пройти по `files` и проверить каждое `fh.Size` до чтения, отсекая нереальные размеры.

---

### M-3. Markdown с `html.WithUnsafe()` + CSP `script-src 'unsafe-inline'` → stored XSS при компрометации админа

- **Файлы:**
  - `internal/handlers/helpers.go:153-156` — `goldmark.WithRendererOptions(html.WithUnsafe())`
  - `cmd/dsite/main.go:128` — `script-src 'self' 'unsafe-inline'`
  - `templates/post.html:20`, `templates/index.html:43`, `templates/resume.html:6`, `templates/admin/editor.html:258`, `templates/admin/resume.html:21` — все используют `{{safeHTML .BodyHTML}}`

#### Что
Markdown-парсер настроен с `html.WithUnsafe()`, то есть пропускает любой raw HTML в выходной документ. Сохранённый HTML потом выводится без экранирования (`safeHTML`). При этом CSP позволяет `'unsafe-inline'` для скриптов — значит, любой `<script>` в посте будет исполнен.

Это **stored XSS, доступный только админу** (потому что только аутентифицированный админ может писать посты). Однако:
1. У вас одно главное «место с ключами» — сессионная кука. Атакующий, получивший доступ к сессии (например, физически сел за ноутбук), может оставить хитрый пост с XSS, который сработает у вас же при просмотре. Поскольку кука `HttpOnly`, угнать сессию через `document.cookie` нельзя — но можно делать действия от лица админа (создать второго юзера, удалить посты и т. д.).
2. Если в будущем появится несколько админов — XSS друг друга уже актуален.
3. Социальная инженерия: вам присылают «готовый markdown», вы вставляете → XSS.

#### Как чинить
Минимум — ограничить CSP, чтобы скрипты не выполнялись inline:
1. Перенести inline JS из `templates/base.html` (lightbox), `templates/gallery.html` (justify-layout), `templates/admin/base.html`, `templates/admin/editor.html`, `templates/admin/gallery.html` в файлы под `static/js/...`, подключить через `<script src=...>`.
2. Снять `'unsafe-inline'` из `script-src`. Inline `style=` остаются — для CSS, но это менее опасно. Если хочешь убрать и `'unsafe-inline'` для стилей, перенеси все inline-стили в классы CSS-файла (это много работы, но даёт защиту от CSS-injection).

```go
// cmd/dsite/main.go
w.Header().Set("Content-Security-Policy",
    "default-src 'self'; "+
        "script-src 'self'; "+
        "style-src 'self' 'unsafe-inline'; "+ // оставляем для inline-styles в шаблонах
        "img-src 'self' data: blob:; "+
        "connect-src 'self'; "+
        "object-src 'none'; "+
        "base-uri 'self'; "+
        "frame-ancestors 'self'; "+
        "form-action 'self'")
```

Опционально (углублённая защита) — отключить `html.WithUnsafe()` и пропускать markdown-вывод через санитайзер `bluemonday`:

```go
// helpers.go
import "github.com/microcosm-cc/bluemonday"

var (
    md goldmark.Markdown
    sanitizer = bluemonday.UGCPolicy()
)

func init() {
    sanitizer.AllowElements("video", "source")
    sanitizer.AllowAttrs("controls", "src", "type").OnElements("video", "source")
    // и т. п. — что нужно для постов
}

func RenderMD(src string) (string, error) {
    var buf bytes.Buffer
    if err := md.Convert([]byte(src), &buf); err != nil {
        return "", err
    }
    return sanitizer.Sanitize(buf.String()), nil
}
```

Нужно убрать `html.WithUnsafe()` из конструктора goldmark — но даже без него с goldmark можно случайно протащить опасные конструкции, поэтому санитайзер всё равно нужен.

---

### M-4. Утечка временных multipart-файлов (`r.MultipartForm.RemoveAll()` не вызывается)

- **Файлы:** `internal/handlers/admin.go:117, 181, 305, 519`

#### Что
Когда multipart-форма содержит файл больше memory-лимита, `ParseMultipartForm` создаёт временные файлы в `os.TempDir()`. После обработки нужно вызывать `r.MultipartForm.RemoveAll()` — это не делается. При длительной работе сервера в `/tmp` накапливается мусор, в худшем случае забивает диск.

#### Как чинить
Везде после `ParseMultipartForm` добавить `defer`:

```go
if err := r.ParseMultipartForm(8 << 20); err != nil {
    http.Error(w, "parse form", 400)
    return
}
defer func() {
    if r.MultipartForm != nil {
        r.MultipartForm.RemoveAll()
    }
}()
```

---

## Low

### L-1. Тайминговый оракул логина (нет dummy-bcrypt при отсутствии юзера)

- **Файл:** `internal/handlers/admin.go:64-69`

```go
_, hash, err := db.GetUserByLogin(login)
if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)) != nil {
    RecordLoginFailure(r)
    ...
}
```

#### Что
Если `GetUserByLogin` вернул ошибку (юзера нет) — `hash` пустой, `bcrypt.CompareHashAndPassword` отклоняет почти мгновенно. Если юзер есть — bcrypt считает хеш ~50–100 мс. По разнице во времени можно перебирать существующие логины. Пока админ один, риск минимален, но при добавлении мульти-юзерности станет актуально.

#### Как чинить
Сделать сравнение константным по времени даже на miss:

```go
const dummyBcrypt = "$2a$10$wzmjk5OqeoqW.6G5NqgGq.bxYWf2zPxmWf0w.5b1xv8bQOsdvzdq6" // bcrypt от любой строки

login := r.FormValue("login")
pass := r.FormValue("password")
_, hash, err := db.GetUserByLogin(login)
if err != nil {
    hash = dummyBcrypt
}
if cerr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pass)); cerr != nil || err != nil {
    RecordLoginFailure(r)
    renderFragment(...)
    return
}
```

Сгенерировать `dummyBcrypt` один раз через `bcrypt.GenerateFromPassword([]byte("invalid"), bcrypt.DefaultCost)`.

---

### L-2. `X-Forwarded-Proto` и `Host` доверяются без проверки proxy

- **Файлы:**
  - `internal/handlers/helpers.go:319-325` — `baseURL`
  - `internal/handlers/public.go:296-300` — Feed дублирует ту же логику

```go
func baseURL(r *http.Request) string {
    scheme := "https"
    if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
        scheme = "http"
    }
    return scheme + "://" + r.Host
}
```

#### Что
1. `X-Forwarded-Proto` читается всегда, даже когда `TrustedProxy = false`. Атакующий, посылающий заголовок `X-Forwarded-Proto: https`, может заставить приложение генерировать https-ссылки, когда фактически работает по http (поможет при cache poisoning, путанице в редиректах).
2. `r.Host` тоже не валидируется — Host header injection: `Host: evil.com` → канонические URL, OG-теги, sitemap.xml, feed.xml будут указывать на `evil.com`. Если соединение прошло мимо обратного прокси, это может попасть в кэш CDN, в OG-теги, которые увидит соцсеть-краулер, и т. п.

#### Как чинить
1. Читать `X-Forwarded-Proto` только если `trustedProxy=true`:
   ```go
   func baseURL(r *http.Request) string {
       scheme := "http"
       if r.TLS != nil {
           scheme = "https"
       } else if trustedProxy && r.Header.Get("X-Forwarded-Proto") == "https" {
           scheme = "https"
       }
       return scheme + "://" + r.Host
   }
   ```
2. Хост валидировать против allowlist:
   ```go
   var allowedHosts = map[string]bool{} // заполняется из env SITE_HOST=demenin.ru,demenin.dev

   func validateHost(r *http.Request) bool {
       if len(allowedHosts) == 0 {
           return true // dev-режим
       }
       host := r.Host
       if i := strings.Index(host, ":"); i >= 0 {
           host = host[:i]
       }
       return allowedHosts[host]
   }
   ```
   В `securityHeaders` отбрасывать запросы с `!validateHost(r)` → 421 Misdirected Request.

3. Дублирующий код в `public.go:Feed` заменить вызовом `baseURL(r)` (DRY + единая логика).

---

### L-3. Утечка памяти rate-limiter'а: бесконечно растущая `loginAttempts`-карта

- **Файл:** `internal/handlers/helpers.go:82-143`

#### Что
`loginAttempts map[string]*loginAttempt` чистится только когда у IP истекает `lockoutDuration`. IP, сделавший меньше 5 попыток, а потом ушедший, остаётся в карте навсегда. Атакующий, использующий ботнет, может за пару часов добавить миллионы IP — и съесть память.

#### Как чинить
Добавить периодический GC + при `LoginAllowed` подчищать просроченные:

```go
// helpers.go — в Init()
go func() {
    for range time.Tick(10 * time.Minute) {
        cleanLoginAttempts()
    }
}()

func cleanLoginAttempts() {
    loginMu.Lock()
    defer loginMu.Unlock()
    now := time.Now()
    for ip, a := range loginAttempts {
        // если последняя активность > loginWindow назад и не залочен — удаляем
        if a.lockedAt.IsZero() && now.Sub(a.firstAt) > loginWindow {
            delete(loginAttempts, ip)
        } else if !a.lockedAt.IsZero() && now.Sub(a.lockedAt) > lockoutDuration {
            delete(loginAttempts, ip)
        }
    }
}
```

Опционально — лимит размера карты (например, 10 000 IP), и при превышении сбрасывать самых старых.

---

### L-4. Установка админа (`/admin/setup`) без какого-либо доказательства владения

- **Файл:** `internal/handlers/admin.go:21-48`

#### Что
Любой, кто откроет `/admin/setup` до того, как реальный владелец это сделал, становится админом. На голом сервере, который пинают сканеры за миллисекунды до владельца, шанс существует. Особенно опасно при автодеплое — публичный URL появляется раньше, чем владелец дойдёт до setup.

В `CLAUDE.md` это формализовано как «фича первого визита», но это не security best practice.

#### Как чинить
Вариант 1 (минимум) — setup-токен из ENV:
```go
// config.go
SetupToken: os.Getenv("SETUP_TOKEN")
// admin.go Setup
if r.URL.Query().Get("token") != cfg.SetupToken || cfg.SetupToken == "" {
    http.NotFound(w, r); return
}
```
Деплой: `SETUP_TOKEN=$(openssl rand -hex 16) docker compose up`, потом владелец заходит на `/admin/setup?token=...`.

Вариант 2 — bind setup-эндпоинта на 127.0.0.1 или Unix-socket: при старте, если юзера нет, поднимать `/admin/setup` только на отдельном listener, доступном через SSH-туннель.

Вариант 3 — печатать одноразовый токен в `log.Println` при первом старте, требовать его в форме setup'а.

---

### L-5. `secureCookies` опционален — небезопасный дефолт

- **Файлы:**
  - `internal/config/config.go:24` — `SecureCookies: os.Getenv("SECURE_COOKIES") == "true"`
  - `internal/handlers/admin.go:77` — `Secure: secureCookies`

#### Что
Если `SECURE_COOKIES` не задан или указан как-то не «true», сессионная кука выставляется без `Secure` флага. Любой, кто запустит контейнер без переменной, получит уязвимую систему. Дефолтом должен быть **secure включён**, явное отключение — для dev.

#### Как чинить
Перевернуть семантику: дефолт = secure, отключение через `INSECURE_COOKIES=true`.
```go
// config.go
SecureCookies: os.Getenv("INSECURE_COOKIES") != "true",
```
Или автодетект по `r.TLS != nil` / `X-Forwarded-Proto: https` (но требует доверия к proxy — см. L-2).

---

### L-6. HSTS отдаётся даже по HTTP

- **Файл:** `cmd/dsite/main.go:125`

#### Что
`Strict-Transport-Security: max-age=31536000; includeSubDomains` — корректный production-заголовок, но он отправляется при любом запросе, в том числе по HTTP. По спецификации браузеры игнорируют HSTS, полученный по http, поэтому проблемы безопасности нет, но это бессмысленно. Хуже — если кто-то ставит сайт за nginx без HTTPS, заголовок остаётся и может смутить сетевые инструменты безопасности.

#### Как чинить
Отдавать HSTS только при https:
```go
if r.TLS != nil || (trustedProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
    w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
}
```
Также добавить `; preload` после того, как сайт пройдёт https://hstspreload.org/.

---

### L-7. Нет защиты от content-injection в `LIKE` (SQL wildcard)

- **Файл:** `internal/db/queries.go:600`

```go
rows, err := DB.Query(`SELECT title FROM posts WHERE cover=? OR body_md LIKE ?`,
    filename, "%"+filename+"%")
```

#### Что
Параметр `filename` приходит из листинга каталога (`AdminUploads` → `entry.Name()`), это безопасный источник. Но `LIKE` интерпретирует `%` и `_` как wildcards. Если в `uploads/` каким-то образом окажется файл с именем, содержащим `%`, запрос вернёт ложноположительные совпадения и пользователю покажут «файл используется» там, где это не так. Не security-bug в строгом смысле, но вектор для логических атак, если в будущем появится прямая загрузка без `saveUpload`.

#### Как чинить
Экранировать LIKE-метасимволы:
```go
escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(filename)
rows, err := DB.Query(
    `SELECT title FROM posts WHERE cover=? OR body_md LIKE ? ESCAPE '\'`,
    filename, "%"+escaped+"%")
```

---

## Info / Hardening

### I-1. CSP позволяет `unsafe-inline` для скриптов и стилей

См. M-3 — это связанная находка. Все inline-`<script>` и inline-`<style>` блоки в шаблонах требуют `'unsafe-inline'`, что отключает большую часть пользы CSP. План: вынести inline-код в файлы и отключить `'unsafe-inline'` хотя бы для `script-src`.

Альтернативно — использовать nonce (`script-src 'self' 'nonce-RANDOM'`), но придётся передавать nonce в каждый шаблон.

---

### I-2. CSRF защищён только Origin/Referer (нет CSRF-токенов)

- **Файл:** `cmd/dsite/main.go:139-161`

Текущая защита: `Origin` или `Referer` должен совпадать по host с `r.Host`. SameSite=Lax + это покрывает 99 % случаев. Однако:
- Origin не отправляется на `<form>`-навигации в некоторых старых браузерах;
- Если за прокси `r.Host` подмешан Host-header injection (см. L-2) — проверка может ложно успешно проходить;
- Защита от XSS не помогает (XSS обходит SameSite).

Не критично для текущего сценария, но усилит — добавить per-session CSRF-токен:
1. При `NewSession()` генерировать второй случайный токен `csrf` и хранить рядом с сессией.
2. Печатать его в `<meta name="csrf">` или скрытом поле формы.
3. В `csrfCheck` валидировать `r.FormValue("csrf")` или `r.Header.Get("X-CSRF-Token")`.

---

### I-3. Нет endpoint'а смены пароля и инвалидации сессий

В `db.UpdatePassword` есть, но никто не дёргает. При компрометации единственный способ смены пароля — лезть в SQLite вручную. Также нет логаута со всех устройств.

Добавить страницу `/admin/account` с:
- сменой пароля (требует ввода текущего);
- кнопкой «выйти со всех устройств» — `DELETE FROM sessions`;
- автоматической инвалидацией всех сессий после смены пароля.

---

### I-4. Слабая `bcrypt.DefaultCost = 10`

- **Файл:** `internal/handlers/admin.go:34`

В 2026 году рекомендация — bcrypt cost 12, либо переход на argon2id (`golang.org/x/crypto/argon2`). На VPS с одним юзером разница незаметна (50 мс vs 300 мс при логине), а защита от offline-перебора при утечке БД заметно лучше.

```go
hash, err := bcrypt.GenerateFromPassword([]byte(pass), 12)
```

---

### I-5. Нет лимита числа файлов в одной загрузке и числа постов на странице

- **Файл:** `internal/handlers/admin.go:310` (`r.MultipartForm.File["photos"]`)
- **Файл:** `internal/handlers/public.go:39` (Index, нет проверки `pageNum > totalPages`)

`UploadPhoto` циклится по `files` без проверки длины. Если кто-то под админом загрузит 10 000 файлов в одном запросе — каждый прогон вызывает `io.ReadAll`, `magic byte check`, запись на диск, `INSERT`. С учётом отсутствия таймаутов (M-1) и MaxBytesReader (M-2) это может затянуться надолго.

```go
const maxFilesPerUpload = 50
if len(files) > maxFilesPerUpload {
    http.Error(w, "слишком много файлов за один раз", 400)
    return
}
```

В `Index` имеет смысл ограничить `pageNum`:
```go
if pageNum > 10000 { pageNum = 10000 }
```
(SQLite справится, но это на всякий случай.)

---

### I-6. Утилита `cmd/recompress` запускает внешний `cwebp`

- **Файл:** `cmd/recompress/main.go:52`

```go
cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", *quality), "-quiet", path, "-o", tmp)
```

Аргументы передаются массивом, а не через shell — command injection невозможен. `path` приходит из `os.ReadDir(*dir)` — если каталог содержит файл с именем, начинающимся с `-`, `cwebp` интерпретирует его как флаг. Маловероятно (имена генерируются `saveUpload`), но добавить разделитель `--`:

```go
cmd := exec.Command("cwebp",
    "-q", fmt.Sprintf("%d", *quality),
    "-quiet",
    "--", path, "-o", tmp,
)
```

И/или валидировать `e.Name()`: префикс — цифра (timestamp).

---

## Сводная таблица — что сделать в первую очередь

| # | Задача | Приоритет | Оценка |
|---|--------|-----------|--------|
| 1 | H-1: Закрыть листинг `/uploads/` (middleware) | High | 15 мин |
| 2 | M-1: Добавить таймауты на `http.Server` | Medium | 15 мин |
| 3 | M-2: `MaxBytesReader` на все upload-эндпоинты | Medium | 30 мин |
| 4 | M-4: `defer r.MultipartForm.RemoveAll()` | Medium | 10 мин |
| 5 | M-3 (часть 1): Перенести inline JS в файлы, убрать `'unsafe-inline'` из script-src | Medium | 2–3 ч |
| 6 | M-3 (часть 2): Добавить `bluemonday` санитайзер для markdown | Medium | 1 ч |
| 7 | L-1: dummy-bcrypt при login miss | Low | 10 мин |
| 8 | L-2: Валидация Host + защита X-Forwarded-Proto | Low | 30 мин |
| 9 | L-3: Cleanup goroutine для `loginAttempts` | Low | 15 мин |
| 10 | L-4: SETUP_TOKEN | Low | 30 мин |
| 11 | L-5: Перевернуть дефолт `SecureCookies` | Low | 10 мин |
| 12 | L-6: HSTS только по HTTPS | Low | 5 мин |
| 13 | L-7: LIKE escaping в `GetFileUsage` | Low | 5 мин |
| 14 | I-3: Страница смены пароля + logout-all | Hardening | 1–2 ч |
| 15 | I-4: bcrypt.cost=12 (или argon2id) | Hardening | 5 мин |
| 16 | I-5: Лимиты на число файлов / страниц | Hardening | 15 мин |
| 17 | I-6: `--` перед путями в `recompress` | Hardening | 2 мин |

**Минимальный sprint-1 (≈ 1.5 часа):** пункты 1, 2, 3, 4, 7, 9, 11, 12, 13.
**Sprint-2 (≈ 4 часа):** пункты 5, 6, 8, 10.
**Sprint-3 (≈ 2 часа):** пункты 14, 15, 16, 17.

---

## Что проверено и признано безопасным

Чтобы исполнитель понимал, где **не нужно** ничего менять:

- **SQL-инъекции:** все запросы в `internal/db/queries.go` параметризованы (`?`), включая динамические условия в `ListPostsPaginated` и `ListPhotos`. FTS-поиск (`SearchPosts`) очищает спецсимволы перед `MATCH ?` — параметр связан, инъекция невозможна.
- **Path traversal:** в обоих эндпоинтах удаления файлов используется `filepath.Base(...)` поверх пользовательского ввода (`AddToGallery:257`, `DeleteUploadFile:682`).
- **Аутентификация:** bcrypt-хэши, сессионные токены 128 бит из `crypto/rand`, кук с `HttpOnly` + `SameSite=Lax`, expiry 30 дней, очистка просроченных в `CleanExpiredSessions`.
- **Rate limiting логина:** 5 попыток за 15 минут на IP (только утечка памяти L-3).
- **Cover/photo upload:** проверка allowlist расширений + magic bytes (`isAllowedImageContent`) + WebP RIFF-валидация — атаки через polyglot-файлы и MIME-confusion блокированы (X-Content-Type-Options: nosniff усиливает).
- **Auto-escaping шаблонов:** Go `html/template` корректно экранирует все `{{...}}` кроме `{{safeHTML ...}}` (где это намеренно). URL-контексты в `<a href>` блокируют `javascript:` URI.
- **Cookies при логауте:** `MaxAge: -1` — корректно для удаления.
- **CSRF:** `csrfCheck` middleware + SameSite=Lax — стандартный современный подход.
- **Заголовки безопасности:** `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, `Referrer-Policy: strict-origin-when-cross-origin`, базовая CSP — всё на месте.
- **Docker:** не root (uid 1000), CGO отключён, образ multi-stage.
- **Секреты в репо:** не найдены — `.gitignore` корректно исключает `*.db`, `uploads/`, `.env*`, `.idea/`.
- **GitHub Actions:** использует `secrets.*`, прав минимум (`contents: read, packages: write`); `appleboy/ssh-action` — официальный экшен. Заметка: ключи SSH передаются в нём — стандартная практика, риск — компрометация repo.

---

## Контакт по вопросам

Любой пункт исполнитель может пропустить, согласовав с владельцем. Если в ходе исправлений всплывёт что-то новое — добавляйте сюда же.
