# Установка Matrix (Synapse) + Element

Самохостируемый мессенджер для семьи и друзей.
Клиент — приложение **Element** (App Store / Google Play).

Все команды выполняются на сервере: `ssh beget`

---

## 1. Остановить Mattermost (если запущен)

```bash
cd /opt/mattermost && docker compose down
```

---

## 2. Создать папки и сгенерировать конфиг

Замени `chat.yourdomain.ru` на свой поддомен.

```bash
mkdir -p /opt/matrix/data

docker run -it --rm \
  -v /opt/matrix/data:/data \
  -e SYNAPSE_SERVER_NAME=chat.yourdomain.ru \
  -e SYNAPSE_REPORT_STATS=no \
  matrixdotorg/synapse:latest generate
```

Создастся файл `/opt/matrix/data/homeserver.yaml`.

---

## 3. Настроить PostgreSQL в конфиге

```bash
nano /opt/matrix/data/homeserver.yaml
```

Найди блок `database:` (там будет sqlite) и **замени целиком** на:

```yaml
database:
  name: psycopg2
  args:
    user: synapse
    password: synapse_password
    database: synapse
    host: postgres
    cp_min: 5
    cp_max: 10
```

Также найди и измени (чтобы закрыть регистрацию для чужих):

```yaml
enable_registration: false
```

Сохрани: `Ctrl+O`, `Enter`, `Ctrl+X`.

---

## 4. Создать docker-compose.yml

```bash
cat > /opt/matrix/docker-compose.yml << 'EOF'
services:
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    volumes:
      - /opt/matrix/db:/var/lib/postgresql/data
    environment:
      - POSTGRES_USER=synapse
      - POSTGRES_PASSWORD=synapse_password
      - POSTGRES_DB=synapse
      - POSTGRES_INITDB_ARGS=--encoding=UTF-8 --lc-collate=C --lc-ctype=C
    mem_limit: 256m

  synapse:
    image: matrixdotorg/synapse:latest
    restart: unless-stopped
    depends_on:
      - postgres
    ports:
      - "127.0.0.1:8008:8008"
    volumes:
      - /opt/matrix/data:/data
    mem_limit: 512m
EOF
```

---

## 5. Права на папку

```bash
chown -R 991:991 /opt/matrix/data
```

---

## 6. Обновить Caddy

```bash
nano /etc/caddy/Caddyfile
```

Блок для чата должен выглядеть так:

```
chat.yourdomain.ru {
    reverse_proxy 127.0.0.1:8008
}
```

Перезагрузить Caddy:

```bash
systemctl reload caddy
```

---

## 7. Запустить

```bash
cd /opt/matrix && docker compose up -d
```

Следить за запуском:

```bash
docker compose logs -f synapse
```

Готово когда появится: `Synapse now listening on TCP port 8008`

---

## 8. Создать пользователей

Первый пользователь — администратор (`-a`):

```bash
docker exec -it matrix-synapse-1 register_new_matrix_user \
  -c /data/homeserver.yaml \
  -u ИМЯ -p ПАРОЛЬ -a \
  http://localhost:8008
```

Остальные пользователи:

```bash
docker exec -it matrix-synapse-1 register_new_matrix_user \
  -c /data/homeserver.yaml \
  -u ИМЯ -p ПАРОЛЬ --no-admin \
  http://localhost:8008
```

---

## 9. Подключиться с телефона

1. Скачать **Element** из App Store или Google Play
2. Открыть → **Sign in** → **Edit** (рядом с matrix.org) → ввести `https://chat.yourdomain.ru`
3. Войти логином и паролем из шага 8

---

## Управление

```bash
# Статус
cd /opt/matrix && docker compose ps

# Логи
docker compose logs -f synapse

# Перезапуск
docker compose restart synapse

# Обновить до новой версии
docker compose pull && docker compose up -d
```

---

## Бэкап

```bash
tar -czf /root/matrix-backup-$(date +%Y%m%d).tar.gz /opt/matrix/db /opt/matrix/data
```

---

## Частые проблемы

### `permission denied` при старте

```bash
chown -R 991:991 /opt/matrix/data
cd /opt/matrix && docker compose restart synapse
```

### Не удаётся подключиться в Element

Проверь что Caddy проксирует правильно:

```bash
curl https://chat.yourdomain.ru/_matrix/client/versions
```

Должен вернуть JSON со списком версий.

### Добавить пользователя позже

```bash
docker exec -it matrix-synapse-1 register_new_matrix_user \
  -c /data/homeserver.yaml \
  -u ИМЯ -p ПАРОЛЬ --no-admin \
  http://localhost:8008
```
