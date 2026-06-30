package config

import "testing"

func TestLoadEffectiveStoreOverridesEnv(t *testing.T) {
	env := map[string]string{"SHELFARR_URL": "http://env", "SHELFARR_TOKEN": "t", "SCHEDULE": "0 * * * *"}
	settings := map[string]string{"SCHEDULE": "*/15 * * * *", "GUI_PORT": "9000"}
	c, err := LoadEffective(func(k string) string { return env[k] }, settings)
	if err != nil {
		t.Fatal(err)
	}
	if c.Schedule != "*/15 * * * *" {
		t.Fatalf("store should override env schedule, got %q", c.Schedule)
	}
	if c.ShelfarrURL != "http://env" {
		t.Fatalf("env should apply when unset in store, got %q", c.ShelfarrURL)
	}
	if c.GUIPort != "9000" {
		t.Fatalf("GUIPort: %q", c.GUIPort)
	}
}

func TestGUIDefaults(t *testing.T) {
	c, _ := loadFrom(func(k string) string {
		return map[string]string{"SHELFARR_URL": "u", "SHELFARR_TOKEN": "t"}[k]
	})
	if c.GUIPort != "7373" || c.GUIBind != "0.0.0.0" || c.AuthMethod != "forms" || c.AuthRequired != "local" {
		t.Fatalf("gui/auth defaults wrong: %+v", c)
	}
}
