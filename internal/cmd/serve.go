package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/hitzhangjie/ruminate/internal/config"
	"github.com/hitzhangjie/ruminate/internal/query"
	"github.com/hitzhangjie/ruminate/internal/serve"
)

var (
	serveHost string
	servePort int
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the web UI and HTTP API on one port",
	Long: `Start Ruminate's local web server: embedded UI + REST/SSE API together.

Open http://127.0.0.1:8420/ in a browser (default bind; override with flags
or config serve.*).

Wiki selection:
  ruminate serve              # multi-wiki: UI can switch among registered wikis
  ruminate serve --wiki NAME  # pin to one wiki (selector disabled)

Endpoints:
  GET  /                  — web UI (SPA)
  GET  /chat              — AI chat page
  GET  /api/health
  GET  /api/wikis
  GET  /api/stats?wiki=
  POST /api/ask           — body may include "wiki"
  POST /api/ask/stream    — SSE stream

Build the UI into the binary with ` + "`make build`" + `.

Examples:
  ruminate serve
  ruminate serve --port 9000
  ruminate serve --wiki mywiki`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Listen host (default: config serve.host or 127.0.0.1)")
	serveCmd.Flags().IntVar(&servePort, "port", 0, "Listen port (default: config serve.port or 8420)")
}

func runServe(cmd *cobra.Command, args []string) error {
	global, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	host := serveHost
	if host == "" {
		host = global.Serve.Host
	}
	if host == "" {
		host = "127.0.0.1"
	}
	port := servePort
	if port <= 0 {
		port = global.Serve.Port
	}
	if port <= 0 {
		port = 8420
	}

	flagWiki, _ := cmd.Flags().GetString("wiki")
	catalog, defaultName, fixed, err := resolveServeWikis(global, flagWiki)
	if err != nil {
		return err
	}

	hub, err := serve.NewHub(catalog, defaultName, fixed, func(name string) (*query.Engine, error) {
		rt, err := config.ResolveRuntimeConfig(name)
		if err != nil {
			return nil, err
		}
		return query.NewEngine(rt)
	})
	if err != nil {
		return err
	}

	// Warm the default engine so startup fails fast on bad wiki config.
	if _, _, err := hub.Resolve(defaultName); err != nil {
		return fmt.Errorf("opening default wiki %q: %w", defaultName, err)
	}

	srv := serve.New(serve.Config{
		Host: host,
		Port: port,
		Hub:  hub,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "\nReceived %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}

// resolveServeWikis decides catalog / default / fixed pin for the HTTP server.
//
//	--wiki X  → fixed to X only
//	no flag   → all registered wikis; default from config (or sole wiki / first)
func resolveServeWikis(cfg *config.Config, flagWiki string) (catalog []string, defaultName, fixed string, err error) {
	if len(cfg.Wikis) == 0 {
		return nil, "", "", fmt.Errorf("no wikis registered — run 'ruminate init' first")
	}

	all := make([]string, 0, len(cfg.Wikis))
	for _, w := range cfg.Wikis {
		all = append(all, w.Name)
	}

	if flagWiki != "" {
		found := false
		for _, n := range all {
			if n == flagWiki {
				found = true
				break
			}
		}
		if !found {
			return nil, "", "", fmt.Errorf("wiki %q not found. Known: %v", flagWiki, all)
		}
		return []string{flagWiki}, flagWiki, flagWiki, nil
	}

	// Multi-wiki mode (or single wiki without pin).
	defaultName = cfg.DefaultWiki
	if defaultName == "" {
		if len(all) == 1 {
			defaultName = all[0]
		} else {
			// Prefer first registered; UI can switch.
			defaultName = all[0]
		}
	} else {
		ok := false
		for _, n := range all {
			if n == defaultName {
				ok = true
				break
			}
		}
		if !ok {
			return nil, "", "", fmt.Errorf("default_wiki %q is not registered. Known: %v", defaultName, all)
		}
	}
	return all, defaultName, "", nil
}
