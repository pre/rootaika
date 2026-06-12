package main

import "testing"

func TestEnv(t *testing.T) {
	const key = "ROOTAIKA_TEST_ENV_KEY"

	t.Setenv(key, "")
	if got := env(key, "fallback"); got != "fallback" {
		t.Fatalf("empty env should use fallback, got %q", got)
	}

	t.Setenv(key, "value")
	if got := env(key, "fallback"); got != "value" {
		t.Fatalf("set env should win, got %q", got)
	}
}
