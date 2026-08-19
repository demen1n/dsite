package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	for _, k := range []string{
		"PORT", "DB_PATH", "UPLOADS_DIR", "SITE_TITLE", "SITE_DESC", "INSECURE_COOKIES", "TRUSTED_PROXY",
		"BACKUP_DIR", "BACKUP_KEEP", "BACKUP_INTERVAL", "BACKUP_REMOTE", "BACKUP_REMOTE_DIR", "YADISK_TOKEN",
	} {
		os.Unsetenv(k)
	}
	c := LoadConfig()
	if c.Port != "8080" {
		t.Errorf("Port: got %q, want %q", c.Port, "8080")
	}
	if c.DBPath != "./data.db" {
		t.Errorf("DBPath: got %q, want %q", c.DBPath, "./data.db")
	}
	if c.UploadsDir != "./uploads" {
		t.Errorf("UploadsDir: got %q, want %q", c.UploadsDir, "./uploads")
	}
	if c.SiteTitle != "My Blog" {
		t.Errorf("SiteTitle: got %q, want %q", c.SiteTitle, "My Blog")
	}
	if !c.SecureCookies {
		t.Error("SecureCookies: want true by default")
	}
	if c.TrustedProxy {
		t.Error("TrustedProxy: want false by default")
	}
	if c.BackupDir != "./backups" {
		t.Errorf("BackupDir: got %q, want %q", c.BackupDir, "./backups")
	}
	if c.BackupKeep != 7 {
		t.Errorf("BackupKeep: got %d, want 7", c.BackupKeep)
	}
	if c.BackupInterval != 24*time.Hour {
		t.Errorf("BackupInterval: got %v, want 24h", c.BackupInterval)
	}
	if c.BackupRemote != "" {
		t.Errorf("BackupRemote: got %q, want empty", c.BackupRemote)
	}
	if c.BackupRemoteDir != "/dsite-backups" {
		t.Errorf("BackupRemoteDir: got %q, want %q", c.BackupRemoteDir, "/dsite-backups")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("SITE_TITLE", "Test Blog")
	os.Setenv("INSECURE_COOKIES", "true")
	os.Setenv("TRUSTED_PROXY", "true")
	os.Setenv("BACKUP_DIR", "/tmp/backups")
	os.Setenv("BACKUP_KEEP", "3")
	os.Setenv("BACKUP_INTERVAL", "6h")
	os.Setenv("BACKUP_REMOTE", "yadisk")
	os.Setenv("BACKUP_REMOTE_DIR", "/mybackups")
	os.Setenv("YADISK_TOKEN", "tok123")
	t.Cleanup(func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DB_PATH")
		os.Unsetenv("SITE_TITLE")
		os.Unsetenv("INSECURE_COOKIES")
		os.Unsetenv("TRUSTED_PROXY")
		os.Unsetenv("BACKUP_DIR")
		os.Unsetenv("BACKUP_KEEP")
		os.Unsetenv("BACKUP_INTERVAL")
		os.Unsetenv("BACKUP_REMOTE")
		os.Unsetenv("BACKUP_REMOTE_DIR")
		os.Unsetenv("YADISK_TOKEN")
	})

	c := LoadConfig()
	if c.Port != "9090" {
		t.Errorf("Port: got %q, want %q", c.Port, "9090")
	}
	if c.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath: got %q, want %q", c.DBPath, "/tmp/test.db")
	}
	if c.SiteTitle != "Test Blog" {
		t.Errorf("SiteTitle: got %q, want %q", c.SiteTitle, "Test Blog")
	}
	if c.SecureCookies {
		t.Error("SecureCookies: want false when INSECURE_COOKIES=true")
	}
	if !c.TrustedProxy {
		t.Error("TrustedProxy: want true when TRUSTED_PROXY=true")
	}
	if c.BackupDir != "/tmp/backups" {
		t.Errorf("BackupDir: got %q, want %q", c.BackupDir, "/tmp/backups")
	}
	if c.BackupKeep != 3 {
		t.Errorf("BackupKeep: got %d, want 3", c.BackupKeep)
	}
	if c.BackupInterval != 6*time.Hour {
		t.Errorf("BackupInterval: got %v, want 6h", c.BackupInterval)
	}
	if c.BackupRemote != "yadisk" {
		t.Errorf("BackupRemote: got %q, want %q", c.BackupRemote, "yadisk")
	}
	if c.BackupRemoteDir != "/mybackups" {
		t.Errorf("BackupRemoteDir: got %q, want %q", c.BackupRemoteDir, "/mybackups")
	}
	if c.YaDiskToken != "tok123" {
		t.Errorf("YaDiskToken: got %q, want %q", c.YaDiskToken, "tok123")
	}
}
