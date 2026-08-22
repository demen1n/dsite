package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port          string
	DBPath        string
	UploadsDir    string
	SiteTitle     string
	SiteDesc      string
	SiteURL       string // e.g. https://demenin.ru — overrides auto-detected base URL
	SecureCookies bool
	TrustedProxy  bool // если true — доверяем X-Forwarded-For (только за обратным прокси)

	BackupDir        string        // local directory for backup archives
	BackupKeep       int           // local archives to retain; 0 keeps all
	BackupInterval   time.Duration // how often to run an automatic backup while the server runs; 0 disables it
	BackupRemote     string        // off-site backend: "" (local only) or "yadisk"
	BackupRemoteDir  string        // destination path/folder on the remote backend
	BackupRemoteKeep int           // remote archives to retain; 0 keeps all (unlike local, nothing prunes the remote otherwise)
	YaDiskToken      string        // Yandex Disk OAuth token, required when BackupRemote == "yadisk"
}

func LoadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8080"),
		DBPath:        getEnv("DB_PATH", "./data.db"),
		UploadsDir:    getEnv("UPLOADS_DIR", "./uploads"),
		SiteTitle:     getEnv("SITE_TITLE", "My Blog"),
		SiteDesc:      getEnv("SITE_DESC", "Фото и заметки"),
		SiteURL:       os.Getenv("SITE_URL"),
		SecureCookies: os.Getenv("INSECURE_COOKIES") != "true",
		TrustedProxy:  os.Getenv("TRUSTED_PROXY") == "true",

		BackupDir:        getEnv("BACKUP_DIR", "./backups"),
		BackupKeep:       getEnvInt("BACKUP_KEEP", 7),
		BackupInterval:   getEnvDuration("BACKUP_INTERVAL", 24*time.Hour),
		BackupRemote:     os.Getenv("BACKUP_REMOTE"),
		BackupRemoteDir:  getEnv("BACKUP_REMOTE_DIR", "/dsite-backups"),
		BackupRemoteKeep: getEnvInt("BACKUP_REMOTE_KEEP", 30),
		YaDiskToken:      os.Getenv("YADISK_TOKEN"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
