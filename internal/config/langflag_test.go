package config

import "testing"

func TestLangInferenceDefaultOnAndOff(t *testing.T) {
	base := map[string]string{"SHELFARR_URL": "u", "SHELFARR_TOKEN": "t"}
	on, err := loadFrom(func(k string) string { return base[k] })
	if err != nil || !on.LangInference {
		t.Fatalf("default should be on: %+v err=%v", on, err)
	}
	base["LANG_INFERENCE"] = "off"
	off, _ := loadFrom(func(k string) string { return base[k] })
	if off.LangInference {
		t.Fatal("LANG_INFERENCE=off should disable")
	}
}
