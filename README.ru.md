# dsite

[![CI](https://github.com/demen1n/dsite/actions/workflows/ci.yml/badge.svg)](https://github.com/demen1n/dsite/actions/workflows/ci.yml)
[![Coverage](https://codecov.io/gh/demen1n/dsite/graph/badge.svg)](https://codecov.io/gh/demen1n/dsite)
[![Go Report Card](https://goreportcard.com/badge/github.com/demen1n/dsite)](https://goreportcard.com/report/github.com/demen1n/dsite)
[![Release](https://img.shields.io/github/v/release/demen1n/dsite)](https://github.com/demen1n/dsite/releases)
[![License: MIT](https://img.shields.io/github/license/demen1n/dsite)](LICENSE)

Минималистичный движок персонального сайта: посты, фотогалерея, резюме.
Go + HTMX + SQLite. Один бинарь, нулевые зависимости в рантайме, без сборки фронтенда.

[English version](README.md)

## Скриншоты

Главная и галерея с фильтрами по темам и местам:

![Главная](docs/screenshots/home.png)

![Галерея](docs/screenshots/gallery.jpg)

Markdown-редактор с live-превью на HTMX:

![Редактор постов](docs/screenshots/editor.png)

## Запуск локально

```bash
INSECURE_COOKIES=true go run ./cmd/dsite
```

> `INSECURE_COOKIES=true` нужен при запуске по HTTP (localhost) — иначе сессионная кука
> имеет флаг `Secure` и браузер не отправляет её по HTTP, вход в админку не работает.
> В production (за HTTPS-прокси) эта переменная не нужна.

Откройте http://localhost:8080. При первом запуске перейдите на `/admin/setup` — создайте аккаунт.

## Переменные окружения

| Переменная         | По умолчанию     | Описание                                        |
|--------------------|------------------|-------------------------------------------------|
| `PORT`             | `8080`           | Порт сервера                                    |
| `DB_PATH`          | `./data.db`      | Путь к SQLite базе                              |
| `UPLOADS_DIR`      | `./uploads`      | Папка для загрузок                              |
| `SITE_TITLE`       | `My Blog`        | Название сайта                                  |
| `SITE_DESC`        | `Фото и заметки` | Подзаголовок                                    |
| `SITE_URL`         | _(автоопределение)_ | Канонический URL (напр. `https://example.com`) для sitemap/robots/фида. В production обязательно. |
| `INSECURE_COOKIES` | —                | `true` для локального запуска по HTTP           |
| `TRUSTED_PROXY`    | —                | `true` если сервер за обратным прокси (доверять `X-Forwarded-For` для рейт-лимита логина) |
| `BACKUP_DIR`        | `./backups`      | Локальная папка для архивов бэкапа (`dsite backup`, см. ниже) |
| `BACKUP_KEEP`       | `7`              | Сколько локальных архивов хранить; старые удаляются при ротации |
| `BACKUP_INTERVAL`   | `24h`            | Как часто работающий сервер бэкапится сам; `0` отключает |
| `BACKUP_REMOTE`     | —                | Куда дополнительно заливать архив: `yadisk` (пусто = только локально) |
| `BACKUP_REMOTE_DIR` | `/dsite-backups` | Папка назначения на удалённом бэкенде |
| `BACKUP_REMOTE_KEEP` | `30`           | Сколько архивов хранить на удалённом бэкенде; `0` — хранить всё |
| `YADISK_TOKEN`      | —                | OAuth-токен Яндекс.Диска (нужен при `BACKUP_REMOTE=yadisk`) |

## Docker

```bash
docker build -t dsite .
docker run -p 8080:8080 -v dsite_data:/data dsite
```

Или через compose: скопируйте `docker-compose.example.yml` в `docker-compose.yml`,
поправьте значения и запустите `docker compose up -d`. TLS терминируйте любым
обратным прокси (Caddy, nginx) и поставьте `TRUSTED_PROXY=true`.

## Фичи

**Контент**
- Посты с markdown-редактором, live-превью (HTMX), тегами, обложкой и slug'ом
- Серии постов с отдельными страницами
- Галерея с категориями, местами съёмки и drag-and-drop сортировкой
- Резюме в markdown
- Редактируемая главная (markdown, меняется из настроек без пересборки)
- Atom-фид (`/feed.xml`), `sitemap.xml`, `robots.txt`
- Полнотекстовый поиск (SQLite FTS5), работает и для кириллицы

**Админка**
- Браузер загрузок (`/admin/uploads`) — показывает, где используется каждый файл, удаляет неиспользуемые
- Медиапикер для вставки изображений в посты
- Настройки: название сайта, подзаголовок, текст главной, соцсети, видимость резюме
- Slug'и с поддержкой кириллицы (транслитерация в латиницу)

**Загрузка фото**
- Серверный ресайз и перекодирование (учёт EXIF-ориентации, до 1600px, JPEG); GIF хранятся как есть — сохраняется анимация
- Проверка magic bytes поверх allowlist-расширений, случайные hex-имена файлов
- Immutable `Cache-Control` для загрузок (1 год)

**Безопасность**
- CSRF-защита (проверка Origin/Referer на POST)
- Rate limiting на логин (5 попыток / 15 мин на IP)
- Security headers (CSP, X-Frame-Options, Referrer-Policy и др.)
- Сессии в SQLite, 30-дневный срок, очистка по расписанию

## Структура

```
cmd/dsite/main.go          # Точка входа: конфиг, инициализация БД, роутинг
internal/config/config.go  # Конфиг из env
internal/db/
  db.go                    # Инициализация SQLite (WAL) + миграции
  queries.go               # Все запросы к БД (raw SQL, без ORM)
internal/handlers/
  helpers.go               # Шаблонизатор, сессии, markdown, загрузки
  public.go                # Публичные страницы
  admin.go                 # Авторизация + CRUD админки
internal/imgproc/          # Серверная обработка изображений
templates/                 # html/template с блочным наследованием
static/                    # CSS, JS, вендоренный HTMX — без сборки
```

## Зависимости

- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — чистый Go SQLite, без CGO
- [`github.com/yuin/goldmark`](https://github.com/yuin/goldmark) — Markdown → HTML (GFM)
- [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto) — bcrypt
- [HTMX](https://htmx.org) — вендорен в `static/`, без CDN

## Бэкапы

```bash
# Разовый бэкап: снапшот БД (VACUUM INTO, без простоя) + архив с папкой
# загрузок в BACKUP_DIR, с ротацией старых локальных архивов.
dsite backup

# Восстановление: обратная операция — заменяет DB_PATH/UPLOADS_DIR
# содержимым архива. Разрушительно, сначала остановите сервер.
# Спрашивает подтверждение, если не передан --force.
dsite restore ./backups/dsite-backup-20260101-120000.tar.gz
```

Локальная копия пишется всегда. `BACKUP_REMOTE=yadisk` + `YADISK_TOKEN`
дополнительно заливают каждый архив на Яндекс.Диск, там тоже с ротацией —
до `BACKUP_REMOTE_KEEP` архивов. Работающий сервер может бэкапиться сам по
расписанию — см. `BACKUP_INTERVAL` выше.

## Лицензия

[MIT](LICENSE)
