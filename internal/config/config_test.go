package config

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "DB_PATH", "UPLOADS_DIR", "SITE_TITLE", "SITE_DESC", "INSECURE_COOKIES", "TRUSTED_PROXY"} {
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
}

func TestLoadConfigFromEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("SITE_TITLE", "Test Blog")
	os.Setenv("INSECURE_COOKIES", "true")
	os.Setenv("TRUSTED_PROXY", "true")
	t.Cleanup(func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DB_PATH")
		os.Unsetenv("SITE_TITLE")
		os.Unsetenv("INSECURE_COOKIES")
		os.Unsetenv("TRUSTED_PROXY")
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
}
