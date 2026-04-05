# Деплой dsite + Mattermost

Это пошаговая инструкция: от аренды VPS до работающего сайта и чата.
Предполагается, что у тебя есть аккаунт на GitHub и зарегистрированный домен.

---

## Содержание

1. [Аренда VPS на Beget](#1-аренда-vps-на-beget)
2. [Подключение к VPS по SSH](#2-подключение-к-vps-по-ssh)
3. [Установка Docker на VPS](#3-установка-docker-на-vps)
4. [Установка Caddy (обратный прокси + HTTPS)](#4-установка-caddy)
5. [Настройка домена](#5-настройка-домена)
6. [Загрузка кода на GitHub](#6-загрузка-кода-на-github)
7. [Настройка GitHub Actions (автодеплой)](#7-настройка-github-actions)
8. [Первый деплой сайта](#8-первый-деплой-сайта)
9. [Настройка Mattermost](#9-настройка-mattermost)
10. [Итоговая проверка](#10-итоговая-проверка)
11. [Обновление сайта в будущем](#11-обновление-сайта-в-будущем)
12. [Бэкап данных](#12-бэкап-данных)

---

## 1. Аренда VPS на Beget

1. Зайди на [beget.com](https://beget.com) → раздел **VPS/VDS**
2. Выбери конфигурацию: **2 CPU, 2 GB RAM, 30 GB NVMe** (этого хватит на сайт + Mattermost)
3. ОС: **Ubuntu 24.04 LTS**
4. При заказе можно сразу добавить SSH-ключ (если есть) — или сделаем это позже
5. После оплаты придёт письмо с **IP-адресом** и **root-паролем**

> Запиши куда-нибудь IP-адрес сервера — он понадобится на каждом шаге.

---

## 2. Подключение к VPS по SSH

SSH — это способ управлять удалённым сервером через терминал.

### Первый вход (по паролю)

```bash
ssh root@<IP_СЕРВЕРА>
```

Введи пароль из письма. Если спросит "Are you sure you want to continue connecting?" — напечатай `yes`.

### Создать SSH-ключ для автодеплоя

Ключ — это пара файлов: приватный (хранится у тебя) и публичный (кладётся на сервер).
GitHub Actions будет использовать этот ключ, чтобы SSH-иться на VPS автоматически.

На **локальной машине** (не на сервере):

```bash
ssh-keygen -t ed25519 -C "github-actions" -f ~/.ssh/beget_ubuntu
```

Нажми Enter два раза (пустой пароль — нужно для автоматизации).

Получишь два файла:
- `~/.ssh/beget_ubuntu` — приватный (никому не давать)
- `~/.ssh/beget_ubuntu.pub` — публичный

### Добавить публичный ключ на сервер

```bash
ssh-copy-id -i ~/.ssh/beget_ubuntu.pub root@<IP_СЕРВЕРА>
```

Теперь можно подключаться без пароля:

```bash
ssh root@<IP_СЕРВЕРА>
```

---

## 3. Установка Docker на VPS

Docker — это система запуска приложений в изолированных контейнерах. Сайт и Mattermost будут работать каждый в своём контейнере.

Подключись к серверу и выполни:

```bash
curl -fsSL https://get.docker.com | sh
```

Проверь, что Docker установился:

```bash
docker --version
# Должно быть что-то вроде: Docker version 27.x.x
```

---

## 4. Установка Caddy

Caddy — это обратный прокси. Он стоит перед всеми сервисами, принимает запросы из интернета и перенаправляет их нужному контейнеру. Плюс он **автоматически** получает и обновляет SSL-сертификаты (HTTPS).

```bash
apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install caddy -y
```

Создай конфигурацию Caddy — замени `yourdomain.ru` на свой домен и `chat.yourdomain.ru` на поддомен для чата:

```bash
cat > /etc/caddy/Caddyfile << 'EOF'
yourdomain.ru {
    reverse_proxy 127.0.0.1:8080
}

chat.yourdomain.ru {
    reverse_proxy 127.0.0.1:8065
}
EOF
```

Перезапусти Caddy:

```bash
systemctl reload caddy
```

> Caddy сам получит сертификаты от Let's Encrypt, как только DNS начнёт указывать на сервер.

---

## 5. Настройка домена

В личном кабинете регистратора домена найди DNS-настройки и создай **A-записи**:

| Имя | Тип | Значение |
|-----|-----|---------|
| `@` | A | `<IP_СЕРВЕРА>` |
| `chat` | A | `<IP_СЕРВЕРА>` |

После сохранения DNS обновляется за 5–30 минут.

Проверить, что всё прописалось:

```bash
# На локальной машине
ping yourdomain.ru
# Должен показать IP твоего сервера
```

---

## 6. Загрузка кода на GitHub

Если ещё не сделано:

1. Зайди на [github.com](https://github.com) → **New repository**
2. Дай имя, например `dsite`, можно сделать **Private**
3. В терминале на локальной машине, в папке проекта:

```bash
git remote add origin https://github.com/<ТВОЙusername>/dsite.git
git push -u origin main
```

---

## 7. Настройка GitHub Actions

GitHub Actions — это система автодеплоя. При каждом пуше в `main` она:
1. Собирает Docker-образ твоего сайта
2. Публикует его в GitHub Container Registry
3. SSH-ится на VPS и перезапускает контейнер с новым образом

Файл `.github/workflows/deploy.yml` уже есть в проекте. Осталось добавить секреты.

### Добавление секретов

Открой репозиторий на GitHub → **Settings** → **Secrets and variables** → **Actions** → **New repository secret**.

Добавь три секрета:

**`VPS_HOST`** — IP-адрес сервера, например:
```
123.45.67.89
```

**`VPS_USER`** — пользователь SSH:
```
root
```

**`VPS_SSH_KEY`** — содержимое приватного ключа. На локальной машине:
```bash
cat ~/.ssh/beget_ubuntu
```
Скопируй весь вывод (включая строки `-----BEGIN...` и `-----END...`) и вставь в секрет.

### Разрешить публикацию образов

Открой репозиторий → **Settings** → **Actions** → **General** → в разделе **Workflow permissions** выбери **Read and write permissions** → Save.

---

## 8. Первый деплой сайта

### Подготовить папки на VPS

Подключись к серверу:

```bash
ssh root@<IP_СЕРВЕРА>
mkdir -p /opt/dsite /data/uploads
```

### Создать docker-compose.yml на VPS

Скопируй с локальной машины:

```bash
scp docker-compose.yml root@<IP_СЕРВЕРА>:/opt/dsite/
```

Или создай прямо на сервере:

```bash
cat > /opt/dsite/docker-compose.yml << 'EOF'
services:
  dsite:
    image: ghcr.io/<ТВОЙusername>/dsite:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - /data:/data
    environment:
      - SECURE_COOKIES=true
      - SITE_TITLE=Имя Фамилия
      - SITE_DESC=Программист и фотограф
    mem_limit: 256m
    cpus: "0.5"
EOF
```

> Замени `<ТВОЙusername>` на свой GitHub-логин (строчными буквами).

### Запустить деплой

Сделай пуш в `main` на локальной машине:

```bash
git commit --allow-empty -m "trigger deploy"
git push
```

Или запусти вручную: GitHub → репозиторий → **Actions** → **Deploy** → **Run workflow**.

Деплой занимает ~2 минуты. После этого открой `https://yourdomain.ru/admin/setup` и создай аккаунт администратора.

---

## 9. Настройка Mattermost

Mattermost — мессенджер с открытым исходным кодом, аналог Slack. Запустим его рядом с сайтом на том же VPS.

### Создать папку и конфиг

На сервере:

```bash
mkdir -p /opt/mattermost
```

Создай файл `/opt/mattermost/docker-compose.yml`:

```bash
cat > /opt/mattermost/docker-compose.yml << 'EOF'
services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    volumes:
      - /opt/mattermost/db:/var/lib/postgresql/data
    environment:
      - POSTGRES_USER=mattermost
      - POSTGRES_PASSWORD=измени_на_случайный_пароль
      - POSTGRES_DB=mattermost
    mem_limit: 256m

  mattermost:
    image: mattermost/mattermost-team-edition:latest
    restart: unless-stopped
    depends_on:
      - postgres
    ports:
      - "127.0.0.1:8065:8065"
    volumes:
      - /opt/mattermost/data:/mattermost/data
      - /opt/mattermost/logs:/mattermost/logs
      - /opt/mattermost/config:/mattermost/config
      - /opt/mattermost/plugins:/mattermost/plugins
    environment:
      - MM_SQLSETTINGS_DRIVERNAME=postgres
      - MM_SQLSETTINGS_DATASOURCE=postgres://mattermost:измени_на_случайный_пароль@postgres/mattermost?sslmode=disable
      - MM_SERVICESETTINGS_SITEURL=https://chat.yourdomain.ru
    mem_limit: 512m
EOF
```

> Обязательно замени `измени_на_случайный_пароль` на что-то нормальное в обоих местах, и `chat.yourdomain.ru` на свой поддомен.

### Запустить Mattermost

```bash
cd /opt/mattermost
docker compose up -d
```

Подожди 30 секунд и открой `https://chat.yourdomain.ru` — появится мастер настройки Mattermost. Создай аккаунт администратора.

### Полезные настройки Mattermost после установки

В **System Console** (иконка меню → System Console):
- **Site Configuration → Users and Teams** → установи лимит команды (например, 10 человек для семьи)
- **Authentication → Email** → можно отключить регистрацию (Enable Open Server = false), приглашения будут только через ссылку
- **Site Configuration → Notifications** → настрой email-уведомления, если нужно

---

## 10. Итоговая проверка

После всех шагов должно работать:

| URL | Что там |
|-----|---------|
| `https://yourdomain.ru` | Твой сайт |
| `https://yourdomain.ru/admin` | Панель управления сайтом |
| `https://chat.yourdomain.ru` | Mattermost (семейный чат) |

Проверить статус контейнеров:

```bash
# Сайт
cd /opt/dsite && docker compose ps

# Mattermost
cd /opt/mattermost && docker compose ps
```

Посмотреть логи, если что-то не так:

```bash
# Логи сайта
cd /opt/dsite && docker compose logs -f

# Логи Mattermost
cd /opt/mattermost && docker compose logs -f mattermost
```

---

## 11. Обновление сайта в будущем

Просто пушишь изменения в `main`:

```bash
git add .
git commit -m "что-то изменил"
git push
```

GitHub Actions сам соберёт образ и задеплоит. Занимает ~2 минуты.

Следить за процессом можно в GitHub → **Actions**.

Обновить Mattermost вручную:

```bash
cd /opt/mattermost
docker compose pull
docker compose up -d
```

---

## 12. Бэкап данных

Все важные данные хранятся в:
- `/data` — SQLite база и фото сайта
- `/opt/mattermost` — база postgres и данные Mattermost

Создать бэкап:

```bash
tar -czf /root/backup-$(date +%Y%m%d).tar.gz /data /opt/mattermost/db /opt/mattermost/data
```

Скачать на локальную машину:

```bash
# На локальной машине
scp root@<IP_СЕРВЕРА>:/root/backup-*.tar.gz ~/backups/
```

Восстановить из бэкапа:

```bash
# На сервере (осторожно — перезапишет текущие данные)
tar -xzf backup-20250101.tar.gz -C /
cd /opt/dsite && docker compose restart
cd /opt/mattermost && docker compose restart
```

> Рекомендую делать бэкап раз в неделю через cron:
> ```bash
> crontab -e
> # Добавить строку:
> 0 3 * * 0 tar -czf /root/backup-$(date +\%Y\%m\%d).tar.gz /data /opt/mattermost/db /opt/mattermost/data 2>/dev/null
> ```

---

## Частые проблемы

### GitHub Actions: `go.mod requires go >= X (running go Y)`

`go mod tidy` на локальной машине обновил версию Go в `go.mod`, а образ в Dockerfile использует старую версию.

Исправить — поменять версию образа в `Dockerfile`:

```dockerfile
FROM golang:1.26-bookworm AS builder
```

Версия должна совпадать с тем, что написано в первой строке `go.mod`.

---

### `docker compose up`: `error from registry: denied`

Образ в ghcr.io приватный, сервер не авторизован его скачивать.

Создай Personal Access Token на GitHub: **Settings → Developer settings → Personal access tokens → Tokens (classic) → Generate new token**, галочка `read:packages`.

На сервере:

```bash
echo "ВАШ_ТОКЕН" | docker login ghcr.io -u ВАШ_GITHUB_LOGIN --password-stdin
```

После этого повтори `docker compose up -d`. Авторизация сохраняется навсегда, повторять не нужно.

---

### Контейнер падает: `unable to open database file: out of memory (14)`

Несмотря на текст ошибки, памяти тут достаточно — это SQLite не может создать файл БД. Причина: папка `/data` принадлежит root, а контейнер работает от пользователя с uid 1000.

```bash
chown -R 1000:0 /data
cd /opt/dsite && docker compose restart
```
