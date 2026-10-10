package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/truenas-exporter/internal/collector"
	"github.com/USA-RedDragon/truenas-exporter/internal/config"
	"github.com/USA-RedDragon/truenas-exporter/internal/truenas"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/ztrue/shutdown"
)

func NewCommand(version, commit string) *cobra.Command {
	return &cobra.Command{
		Use:     "truenas-exporter",
		Short:   "Prometheus exporter for TrueNAS pool, disk, SMART, alert and ARC state",
		Version: fmt.Sprintf("%s - %s", version, commit),
		Annotations: map[string]string{
			"version": version,
			"commit":  commit,
		},
		RunE:              runRoot,
		SilenceErrors:     true,
		DisableAutoGenTag: true,
	}
}

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	c, err := configulator.FromContext[config.Config](cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to get config from context: %w", err)
	}
	cfg, err := c.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	setupLogger(cfg.LogLevel)
	return cfg, nil
}

func setupLogger(level config.LogLevel) {
	var logger *slog.Logger
	switch level {
	case config.LogLevelDebug:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelDebug}))
	case config.LogLevelInfo:
		logger = slog.New(tint.NewTextHandler(os.Stdout, &tint.Options{Level: slog.LevelInfo}))
	case config.LogLevelWarn:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelWarn}))
	case config.LogLevelError:
		logger = slog.New(tint.NewTextHandler(os.Stderr, &tint.Options{Level: slog.LevelError}))
	}
	slog.SetDefault(logger)
}

func runRoot(cmd *cobra.Command, _ []string) error {
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	slog.Info("truenas-exporter", "version", cmd.Annotations["version"], "commit", cmd.Annotations["commit"])

	client := truenas.New(truenas.Options{
		URL:                cfg.TrueNAS.URL,
		APIKey:             cfg.TrueNAS.APIKey,
		Fingerprint:        cfg.TrueNAS.Fingerprint(),
		InsecureSkipVerify: cfg.TrueNAS.InsecureSkipVerify,
		Timeout:            cfg.TrueNAS.Timeout,
	})
	coll := collector.New(client, cfg.Poll.Interval, cfg.Poll.SMARTInterval)

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		coll,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	srv := &http.Server{
		Addr:              net.JoinHostPort(strings.Trim(cfg.HTTP.Bind, "[]"), strconv.Itoa(cfg.HTTP.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go coll.Run(ctx)
	go func() {
		slog.Info("HTTP server started", "address", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	shutdown.AddWithParam(func(sig os.Signal) {
		slog.Info("Received signal", "signal", sig)
		cancel()
		shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("Failed to stop HTTP server", "error", err)
		}
		client.Close()
		slog.Info("Stopped gracefully")
	})
	shutdown.Listen(syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	return nil
}
