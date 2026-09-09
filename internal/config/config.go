package config

import (
	"errors"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL       string
	HTTPAddr          string
	LogLevel          slog.Level
	WorkerConcurrency int
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), HTTPAddr: "127.0.0.1:8080", WorkerConcurrency: 4, ShutdownTimeout: 30 * time.Second}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return c, errors.New("DATABASE_URL must be a postgres:// or postgresql:// URL")
	}
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		c.HTTPAddr = v
	}
	if _, port, err := net.SplitHostPort(c.HTTPAddr); err != nil || port == "" {
		return c, errors.New("HTTP_ADDR must be host:port")
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return c, errors.New("LOG_LEVEL must be debug, info, warn, or error")
		}
	}
	if v := os.Getenv("WORKER_CONCURRENCY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return c, errors.New("WORKER_CONCURRENCY must be between 1 and 100")
		}
		c.WorkerConcurrency = n
	}
	return c, nil
}
