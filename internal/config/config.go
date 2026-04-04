package config

import (
	"os"
)

type Config struct {
	Port          string
	DBPath        string
	UploadsDir    string
	SiteTitle     string
	SiteDesc      string
	SecureCookies bool
	TrustedProxy  bool // если true — доверяем X-Forwarded-For (только за обратным прокси)
}

func LoadConfig() Config {
	return Config{
		Port:          getEnv("PORT", "8080"),
		DBPath:        getEnv("DB_PATH", "./data.db"),
		UploadsDir:    getEnv("UPLOADS_DIR", "./uploads"),
		SiteTitle:     getEnv("SITE_TITLE", "My Blog"),
		SiteDesc:      getEnv("SITE_DESC", "Фото и заметки"),
		SecureCookies: os.Getenv("SECURE_COOKIES") == "true",
		TrustedProxy:  os.Getenv("TRUSTED_PROXY") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
