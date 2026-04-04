# Деплой dsite

## Что происходит автоматически

При каждом пуше в ветку `main`:
1. GitHub Actions собирает Docker-образ
2. Публикует его в GitHub Container Registry (`ghcr.io`)
3. SSH-ится на VPS и перезапускает контейнер с новым образом

---

## Первоначальная настройка

### 1. VPS

Рекомендуемые провайдеры: Timeweb Cloud, Selectel, Hetzner.
Минимум: 1 CPU, 512MB RAM, 10GB диск. ОС: Ubuntu 24.04 LTS.

Установить Docker на VPS:
```bash
curl -fsSL https://get.docker.com | sh
```

### 2. Домен

Купить на reg.ru или nic.ru. В DNS-настройках создать A-запись:
```
@ → <IP вашего VPS>
```
Подождать 5–30 минут на распространение.

### 3. GitHub репозиторий

Залить код на GitHub (репозиторий может быть приватным).

Открыть **Settings → Secrets and variables → Actions** и добавить три секрета:

| Секрет | Значение |
|--------|---------|
| `VPS_HOST` | IP-адрес VPS |
| `VPS_USER` | Пользователь для SSH (например, `root`) |
| `VPS_SSH_KEY` | Приватный SSH-ключ (содержимое `~/.ssh/id_ed25519`) |

Как сгенерировать ключ и добавить на VPS:
```bash
# На локальной машине
ssh-keygen -t ed25519 -C "github-actions"
# Скопировать публичный ключ на VPS
ssh-copy-id -i ~/.ssh/id_ed25519.pub root@<IP>
# Содержимое приватного ключа добавить в секрет VPS_SSH_KEY
cat ~/.ssh/id_ed25519
```

### 4. Папка на VPS

```bash
mkdir -p /opt/dsite /data/uploads
```

Скопировать `docker-compose.yml` на VPS:
```bash
scp docker-compose.yml root@<IP>:/opt/dsite/
```

### 5. Nginx

```bash
apt install nginx certbot python3-certbot-nginx -y
```

Создать файл `/etc/nginx/sites-available/dsite`:
```nginx
server {
    listen 80;
    server_name yourdomain.ru;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /uploads/ {
        alias /data/uploads/;
        expires 30d;
        add_header Cache-Control "public";
    }
}
```

```bash
ln -s /etc/nginx/sites-available/dsite /etc/nginx/sites-enabled/
nginx -t && systemctl reload nginx
```

### 6. HTTPS сертификат

```bash
certbot --nginx -d yourdomain.ru
```

Certbot сам допишет HTTPS-блок и настроит редирект с HTTP. Сертификат обновляется автоматически.

### 7. Первый деплой

Сделать любой пуш в `main` (или запустить workflow вручную через Actions → Deploy → Run workflow).

После деплоя открыть `https://yourdomain.ru/admin/setup` и создать аккаунт администратора.

---

## Переменные окружения

Настраиваются в `docker-compose.yml` на VPS (секция `environment`):

| Переменная | По умолчанию | Описание |
|------------|-------------|---------|
| `SITE_TITLE` | `My Blog` | Название сайта |
| `SITE_DESC` | `Фото и заметки` | Подзаголовок |
| `SECURE_COOKIES` | `true` | Флаг Secure на session cookie (оставить true при HTTPS) |

---

## Обновление сайта

Просто пушить в `main`. Деплой занимает ~2 минуты.

## Бэкап данных

Все данные хранятся в `/data` на VPS (SQLite БД + загруженные фото).

```bash
# Создать бэкап
tar -czf backup-$(date +%Y%m%d).tar.gz /data

# Скопировать локально
scp root@<IP>:~/backup-*.tar.gz .
```
