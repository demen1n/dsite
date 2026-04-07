# Бэкап dsite

Данные живут в Docker volume `myblog_data`, смонтированном в `/data`:
- `/data/data.db` — SQLite база
- `/data/uploads/` — фотографии

## 1. Установить rclone на VPS

```bash
curl https://rclone.org/install.sh | sudo bash
```

## 2. Авторизовать Яндекс.Диск

Авторизацию делать **локально** (нужен браузер):

```bash
rclone authorize "yandex"
```

Скопировать полученный токен. На VPS создать конфиг:

```bash
mkdir -p ~/.config/rclone
nano ~/.config/rclone/rclone.conf
```

```ini
[yadisk]
type = yandex
token = {"access_token":"...","token_type":"OAuth","refresh_token":"...","expiry":"..."}
```

Проверить:

```bash
rclone lsd yadisk:
```

## 3. Скрипт бэкапа

Сохранить в `/root/backup-dsite.sh`:

```bash
#!/bin/bash
set -e

DATE=$(date +%Y-%m-%d_%H-%M)
TMP=/tmp/dsite-backup

mkdir -p "$TMP"

# Горячий бэкап SQLite (безопасно при работающем контейнере)
docker run --rm -v myblog_data:/data alpine \
  sh -c "sqlite3 /data/data.db '.backup /data/backup.db'"

docker cp $(docker ps -qf "ancestor=myblog"):/dev/null /dev/null 2>/dev/null || true
docker run --rm -v myblog_data:/data -v "$TMP":/out alpine \
  cp /data/backup.db /out/db_$DATE.db

# Архив фоток
docker run --rm -v myblog_data:/data -v "$TMP":/out alpine \
  tar czf /out/uploads_$DATE.tar.gz -C /data uploads

# Заливаем на Яндекс.Диск
rclone copy "$TMP/db_$DATE.db" yadisk:Backups/dsite/
rclone copy "$TMP/uploads_$DATE.tar.gz" yadisk:Backups/dsite/

# Чистим локальный tmp и старые бэкапы на диске (старше 30 дней)
rm -rf "$TMP"
rclone delete --min-age 30d yadisk:Backups/dsite/

echo "Backup OK: $DATE"
```

```bash
chmod +x /root/backup-dsite.sh
```

## 4. Cron — раз в день в 3:00

```bash
crontab -e
```

```
0 3 * * * /root/backup-dsite.sh >> /root/backup.log 2>&1
```

## 5. Ручное восстановление

```bash
# Скачать с Яндекс.Диска
rclone copy yadisk:Backups/dsite/db_2025-01-01_03-00.db /tmp/
rclone copy yadisk:Backups/dsite/uploads_2025-01-01_03-00.tar.gz /tmp/

# Восстановить базу
docker run --rm -v myblog_data:/data -v /tmp:/src alpine \
  cp /src/db_2025-01-01_03-00.db /data/data.db

# Восстановить фотки
docker run --rm -v myblog_data:/data -v /tmp:/src alpine \
  tar xzf /src/uploads_2025-01-01_03-00.tar.gz -C /data

# Перезапустить контейнер
docker restart <container_name>
```
