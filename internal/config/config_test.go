package config

import "testing"

func TestLoad(t *testing.T) {
	for _, name := range []string{"DATABASE_URL", "HTTP_ADDR", "LOG_LEVEL", "WORKER_CONCURRENCY"} {
		t.Setenv(name, "")
	}
	t.Setenv("DATABASE_URL", "postgres://localhost/music")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" || cfg.WorkerConcurrency != 4 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	for _, tc := range []struct{ key, value string }{
		{"DATABASE_URL", ""}, {"DATABASE_URL", "https://localhost/music"},
		{"HTTP_ADDR", "localhost"}, {"LOG_LEVEL", "verbose"},
		{"WORKER_CONCURRENCY", "0"}, {"WORKER_CONCURRENCY", "101"}, {"WORKER_CONCURRENCY", "abc"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected invalid configuration to fail")
			}
		})
	}
}
