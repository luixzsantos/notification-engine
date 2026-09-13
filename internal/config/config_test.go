package config

import "testing"

func TestGetEnvAsStringList(t *testing.T) {
	t.Setenv("TEST_STRING_LIST", " a, b ,c")
	got := getEnvAsStringList("TEST_STRING_LIST", []string{"default"})
	want := []string{"a", "b", "c"}

	if len(got) != len(want) {
		t.Fatalf("esperava %v, obteve %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("esperava %v, obteve %v", want, got)
		}
	}
}

func TestGetEnvAsStringList_FallbackWhenUnset(t *testing.T) {
	t.Setenv("TEST_STRING_LIST_UNSET", "")
	got := getEnvAsStringList("TEST_STRING_LIST_UNSET", []string{"*"})
	if len(got) != 1 || got[0] != "*" {
		t.Fatalf("esperava fallback [\"*\"], obteve %v", got)
	}
}

func TestGetEnvAsInt64List(t *testing.T) {
	t.Setenv("TEST_INT64_LIST", "111, 222,abc,333")
	got := getEnvAsInt64List("TEST_INT64_LIST", nil)
	want := []int64{111, 222, 333} // "abc" deve ser ignorado silenciosamente

	if len(got) != len(want) {
		t.Fatalf("esperava %v, obteve %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("esperava %v, obteve %v", want, got)
		}
	}
}

func TestLoad_DefaultsWhenNoEnvSet(t *testing.T) {
	cfg := Load()

	if len(cfg.CORSAllowedOrigins) != 1 || cfg.CORSAllowedOrigins[0] != "*" {
		t.Errorf("esperava CORS default [\"*\"], obteve %v", cfg.CORSAllowedOrigins)
	}
	if cfg.APIKey != "" {
		t.Errorf("esperava APIKey vazio por default, obteve %q", cfg.APIKey)
	}
	if cfg.AllowPrivateNetworkTargets {
		t.Error("esperava AllowPrivateNetworkTargets=false por default (SSRF protegido por padrão)")
	}
}
