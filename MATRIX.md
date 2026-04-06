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

  synapse-admin:
    image: awesometechnologies/synapse-admin:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8009:80"
    mem_limit: 64m
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
    handle /.well-known/matrix/client {
        header Content-Type application/json
        respond `{"m.homeserver":{"base_url":"https://chat.yourdomain.ru"}}` 200
    }
    handle /.well-known/matrix/server {
        header Content-Type application/json
        respond `{"m.server":"chat.yourdomain.ru:443"}` 200
    }
    reverse_proxy 127.0.0.1:8008
}

admin.chat.yourdomain.ru {
    reverse_proxy 127.0.0.1:8009
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

### Первый пользователь (администратор) — через консоль

```bash
docker exec -it matrix-synapse-1 register_new_matrix_user \
  -c /data/homeserver.yaml \
  -u ИМЯ -p ПАРОЛЬ -a \
  http://localhost:8008
```

### Остальные пользователи — через веб-интерфейс

Открой `https://admin.chat.yourdomain.ru`, войди своим логином/паролем и укажи URL сервера `https://chat.yourdomain.ru`. Дальше создавай пользователей через обычный UI — без консоли.

Или через консоль, если удобнее:

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

## Звонки (TURN через coturn)

Для звонков Matrix нужен TURN-сервер — посредник для аудио/видео трафика когда устройства за NAT не могут соединиться напрямую.

> **Важно:** работает с приложением **Element Classic**. Element X использует новый протокол MatrixRTC и требует LiveKit — это отдельная история.

### Добавить coturn в docker-compose.yml

```bash
nano /opt/matrix/docker-compose.yml
```

Добавь сервис в конец (перед закрывающим `EOF` если создаёшь заново):

```yaml
  coturn:
    image: coturn/coturn:latest
    restart: unless-stopped
    network_mode: host
    volumes:
      - /opt/matrix/coturn.conf:/etc/coturn/turnserver.conf
    mem_limit: 64m
```

> `network_mode: host` обязателен — WebRTC использует широкий диапазон UDP-портов, которые нереально пробросить вручную.

### Создать конфиг coturn

```bash
cat > /opt/matrix/coturn.conf << 'EOF'
listening-port=3478
min-port=49152
max-port=65535
fingerprint
use-auth-secret
static-auth-secret=СЛУЧАЙНАЯ_СТРОКА
realm=chat.yourdomain.ru
allowed-peer-ip=IP_СЕРВЕРА
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
total-quota=100
EOF
```

Замени `СЛУЧАЙНАЯ_СТРОКА` (придумай или сгенерируй: `openssl rand -hex 32`) и `IP_СЕРВЕРА` на IP своего VPS.

### Добавить в homeserver.yaml

```bash
nano /opt/matrix/data/homeserver.yaml
```

Добавь в конец файла:

```yaml
turn_uris:
  - "turn:chat.yourdomain.ru:3478?transport=udp"
  - "turn:chat.yourdomain.ru:3478?transport=tcp"
turn_shared_secret: СЛУЧАЙНАЯ_СТРОКА
turn_user_lifetime: 86400000
turn_allow_guests: false
```

> `turn_shared_secret` должен совпадать с `static-auth-secret` в `coturn.conf`.

### Открыть порты

```bash
ufw allow 3478/tcp
ufw allow 3478/udp
ufw allow 49152:65535/udp
```

Если `ufw` неактивен (`ufw status` → `inactive`) — порты уже открыты, ничего делать не нужно.

### Запустить

```bash
cd /opt/matrix && docker compose up -d
docker compose restart synapse
```

### Проверить

В Element Classic: Настройки → Справка и о программе → должен показать TURN-серверы.

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
