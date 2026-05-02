# dsite

Минималистичный персональный сайт: посты, галерея, резюме.  
Go + HTMX + SQLite. Один бинар, нулевые зависимости в рантайме.

## Запуск локально

```bash
go run ./cmd/dsite
```

Откройте http://localhost:8080  
При первом запуске перейдите на `/admin/setup` — создайте аккаунт.

## Переменные окружения

| Переменная     | По умолчанию     | Описание                   |
|----------------|------------------|----------------------------|
| `PORT`         | `8080`           | Порт сервера               |
| `DB_PATH`      | `./data.db`      | Путь к SQLite базе         |
| `UPLOADS_DIR`  | `./uploads`      | Папка для загрузок         |
| `SITE_TITLE`   | `My Blog`        | Название сайта             |
| `SITE_DESC`    | `Фото и заметки` | Подзаголовок               |

## Docker

```bash
docker build -t dsite .
docker run -p 8080:8080 -v dsite_data:/data dsite
```

## Структура

```
cmd/dsite/main.go          # Точка входа, роутинг
internal/config/config.go  # Конфиг из env
internal/db/
  db.go                    # Инициализация SQLite + миграции
  queries.go               # Все запросы к БД
internal/handlers/
  helpers.go               # Шаблонизатор, сессии, утилиты
  public.go                # Публичные страницы
  admin.go                 # Авторизация + CRUD
templates/
  base.html                # Публичный лэйаут (CSS внутри)
  admin/base.html          # Лэйаут админки
  admin/editor.html        # Редактор постов
static/                    # htmx.min.js
uploads/                   # Загруженные файлы
```

## Фичи

**Контент**
- Посты с markdown-редактором, live-превью, тегами, обложкой и slug'ом
- Галерея с категориями, местами съёмки и drag-and-drop сортировкой
- Резюме в markdown
- RSS-лента (`/feed.xml`)
- Полнотекстовый поиск (SQLite FTS5)

**Админка**
- Браузер загрузок (`/admin/uploads`) — показывает где используется каждый файл (галерея, посты), удаление неиспользуемых
- Редактор главной страницы (markdown) — меняется из настроек без пересборки
- Настройки: название сайта, подзаголовок, текст главной, ссылки на соцсети, видимость резюме
- Медиапикер для вставки изображений в посты

**Загрузка фото**
- Клиентский ресайз до 1600px и конвертация в WebP (OffscreenCanvas) перед отправкой
- Проверка magic bytes на сервере, случайные hex-имена файлов
- Immutable Cache-Control для загрузок (1 год)

**Безопасность**
- CSRF-защита (проверка Origin / Referer)
- Rate limiting на логин (5 попыток / 15 мин)
- Security headers (CSP, X-Frame-Options, HSTS и др.)
- Сессии в SQLite, 30-дневный срок, очистка по расписанию

## Зависимости

- `modernc.org/sqlite` — чистый Go SQLite, без CGO
- `github.com/yuin/goldmark` — Markdown → HTML
- `golang.org/x/crypto` — bcrypt
- HTMX 1.9 — подгружен локально, без сборки
