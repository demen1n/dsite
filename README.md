# myblog

Минималистичный персональный сайт: посты, галерея, резюме.  
Go + HTMX + SQLite. Один бинар, нулевые зависимости в рантайме.

## Запуск локально

```bash
# Установить зависимости
go mod tidy

# Запуск
go run .
```

Откройте http://localhost:8080  
При первом запуске перейдите на http://localhost:8080/admin/setup — создайте аккаунт.

## Переменные окружения

| Переменная     | По умолчанию  | Описание                   |
|----------------|---------------|----------------------------|
| `PORT`         | `8080`        | Порт сервера               |
| `DB_PATH`      | `./data.db`   | Путь к SQLite базе         |
| `UPLOADS_DIR`  | `./uploads`   | Папка для фотографий       |
| `SITE_TITLE`   | `My Blog`     | Название сайта             |
| `SITE_DESC`    | `Фото и заметки` | Подзаголовок            |

## Docker

```bash
docker build -t myblog .
docker run -p 8080:8080 -v myblog_data:/data myblog
```

## Структура

```
myblog/
├── main.go                 # Точка входа, роутинг
├── config.go               # Конфиг из env
├── db/
│   ├── db.go              # Инициализация SQLite + миграции
│   └── queries.go         # Все запросы к БД
├── handlers/
│   ├── helpers.go         # Утилиты, сессии, шаблонизатор
│   ├── public.go          # Публичные страницы
│   └── admin.go           # Авторизация + CRUD
├── templates/
│   ├── base.html          # Публичный лэйаут
│   ├── index.html         # Главная
│   ├── post.html          # Страница поста
│   ├── gallery.html       # Галерея
│   ├── resume.html        # Резюме
│   └── admin/             # Шаблоны админки
└── static/                # JS, CSS (если нужно)
```

## Фичи

- **Клиентский ресайз фото** до 1920px WebP перед отправкой (OffscreenCanvas)
- **Markdown-редактор** с live-превью через HTMX
- **Галерея** с лайтбоксом
- **Сессии** в памяти (перезапуск = выход из системы)
- **SQLite WAL** — быстро, надёжно, один файл

## Зависимости

- `github.com/mattn/go-sqlite3` — SQLite драйвер (CGO)
- `github.com/yuin/goldmark` — Markdown → HTML
- `golang.org/x/crypto` — bcrypt для паролей
- HTMX — подгружается с CDN, никакой сборки
