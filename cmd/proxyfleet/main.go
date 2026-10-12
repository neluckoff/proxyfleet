package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/neluckoff/proxyfleet/internal/agent"
	"github.com/neluckoff/proxyfleet/internal/control"
	"github.com/neluckoff/proxyfleet/internal/healthcheck"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	flag.Parse()
	mode := "server"
	if flag.NArg() > 0 {
		mode = flag.Arg(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var err error
	switch mode {
	case "server":
		err = control.Run(ctx, log)
	case "agent":
		err = agent.Run(ctx, log)
	case "healthcheck":
		err = healthcheck.Run(ctx)
	default:
		err = errors.New("usage: proxyfleet [server|agent|healthcheck]")
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("proxyfleet stopped", "mode", mode, "error", err)
		os.Exit(1)
	}
}
