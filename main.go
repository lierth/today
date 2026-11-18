// Command today is the macOS menu-bar app: it starts the local server and
// then hands the main thread to the native event loop.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"today/internal/app"
	"today/internal/host"
)

func main() {
	addr := flag.String("addr", "", "listen address (default: config.json, then "+app.DefaultAddr+")")
	db := flag.String("db", "", "database path")
	dev := flag.Bool("dev", false, "development mode: no funnel, insecure cookies allowed")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *version {
		fmt.Println(host.AppVersion())
		return
	}
	if !host.Claim(*addr) {
		return
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, pw, err := app.Boot(ctx, app.Config{Addr: *addr, DBPath: *db, Dev: *dev, Logger: logger, FallbackPort: true})
	if err != nil {
		logger.Fatalf("today: %v", err)
	}
	defer a.Close()

	h := host.New(a.Server, *dev, logger)
	h.SetShutdown(a.Close)
	if pw != "" {
		h.SetFirstRunPassword(pw)
	}
	if err := h.Run(); err != nil {
		logger.Fatalf("today: %v", err)
	}
}
