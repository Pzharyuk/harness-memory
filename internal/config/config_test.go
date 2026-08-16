package config

import (
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("MEMORY_DATABASE_URL", "")
	t.Setenv("MEMORY_LISTEN", "")
	t.Setenv("HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8741" {
		t.Fatalf("listen=%q", c.Listen)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("MEMORY_DATABASE_URL", "postgres://x")
	t.Setenv("MEMORY_LISTEN", "0.0.0.0:9000")
	t.Setenv("MEMORY_URL", "http://memory.example")
	t.Setenv("MEMORY_TOKEN", "tok")
	t.Setenv("HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.DatabaseURL != "postgres://x" || c.Listen != "0.0.0.0:9000" || c.URL != "http://memory.example" || c.Token != "tok" {
		t.Fatalf("%+v", c)
	}
}
