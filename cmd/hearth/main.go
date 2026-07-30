package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hearth/internal/config"
	"hearth/internal/httpapi"
	"hearth/internal/palworld"
	"hearth/internal/panel"
)

func main() {
	configPath := flag.String("config", "", "path to the JSON configuration file")
	logPath := flag.String("log", "", "path to the Hearth log file")
	flag.Parse()

	logFile, err := setupLogging(*logPath)
	if err != nil {
		slog.Error("initialize logging", "error", err)
		os.Exit(1)
	}
	if logFile != nil {
		defer logFile.Close()
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load configuration", "error", err)
		os.Exit(1)
	}
	cfg.LogFile = *logPath
	var service panel.Service
	if cfg.Demo {
		service = panel.NewDemoService()
	} else {
		if !cfg.Games.Palworld.Enabled {
			slog.Error("palworld adapter must be enabled in production mode")
			os.Exit(1)
		}
		palworldService, adapterErr := palworld.NewService(cfg.Games.Palworld)
		if adapterErr != nil {
			slog.Error("initialize Palworld Windows adapter", "error", adapterErr)
			os.Exit(1)
		}
		service = palworldService
	}
	handler, err := httpapi.New(cfg, service)
	if err != nil {
		slog.Error("initialize HTTP API", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("Hearth listening", "address", cfg.Listen, "demo", cfg.Demo)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve Hearth", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown Hearth", "error", err)
	}
	slog.Info("Hearth stopped")
}

func setupLogging(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, file), nil))
	slog.SetDefault(logger)
	return file, nil
}
