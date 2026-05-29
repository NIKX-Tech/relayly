package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/NIKX-Tech/relayly/internal/admin"
	"github.com/NIKX-Tech/relayly/internal/api"
	"github.com/NIKX-Tech/relayly/internal/config"
	"github.com/NIKX-Tech/relayly/internal/database"
	"github.com/NIKX-Tech/relayly/internal/noise"
	"github.com/NIKX-Tech/relayly/internal/relay"
	"github.com/NIKX-Tech/relayly/pkg/version"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Relayly relay and admin servers",
	RunE:  runStart,
}

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.Flags().Int("port", 8080, "Relay server port")
	startCmd.Flags().String("host", "0.0.0.0", "Relay server host")
	startCmd.Flags().String("db.path", "./data/relayly.db", "Path to database")
	startCmd.Flags().Bool("debug", false, "Enable debug logging")
	startCmd.Flags().Bool("dev", false, "Enable local dev mode (implies debug, console logs)")
}

func runStart(cmd *cobra.Command, args []string) error {
	// ── Config ──────────────────────────────────────────────────────────────
	cfg, err := config.Load(cfgFile, cmd.Flags())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if dev, _ := cmd.Flags().GetBool("dev"); dev {
		cfg.Log.Level = "debug"
		cfg.Log.Format = "console"
	} else if debug, _ := cmd.Flags().GetBool("debug"); debug {
		cfg.Log.Level = "debug"
	}

	// ── Logger ──────────────────────────────────────────────────────────────
	log, err := buildLogger(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()

	log.Info("starting relayly", zap.String("addr", cfg.Addr()))

	// ── Noise keypair ────────────────────────────────────────────────────────
	kp, created, err := noise.LoadOrCreateKeypair(cfg.Noise.KeyPath)
	if err != nil {
		return fmt.Errorf("noise keypair: %w", err)
	}
	if created {
		log.Info("generated new Noise keypair", zap.String("path", cfg.Noise.KeyPath))
	}
	log.Info("noise public key", zap.String("pub", kp.PublicKeyHex()))

	// ── Database ─────────────────────────────────────────────────────────────
	db, err := database.Open(cfg.DB.Path)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()
	log.Info("database opened", zap.String("path", cfg.DB.Path))

	// ── Hub ──────────────────────────────────────────────────────────────────
	hub := relay.NewHub(log)
	go hub.Run()

	// ── Relay HTTP server ─────────────────────────────────────────────────
	relayMux := http.NewServeMux()
	relayMux.Handle("/ws", relay.RateLimitMiddleware(relay.Handler(hub, db, cfg, log, kp)))
	relayMux.HandleFunc("/health", relay.StatusHandler(hub))

	// Mount REST API under /api/
	apiHandler := api.New(db, hub, log, version.Version)
	relayMux.Handle("/api/", apiHandler)

	relayServer := &http.Server{
		Addr:    cfg.Addr(),
		Handler: relayMux,
	}

	// ── Admin HTTP server ─────────────────────────────────────────────────
	var adminServer *http.Server
	if cfg.Admin.Enabled {
		adminSrv, err := admin.New(hub, db, log)
		if err != nil {
			return fmt.Errorf("creating admin server: %w", err)
		}
		adminServer = &http.Server{
			Addr:    cfg.AdminAddr(),
			Handler: adminSrv,
		}
		log.Info("admin UI enabled", zap.String("addr", cfg.AdminAddr()))
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 2)

	go func() {
		log.Info("relay server listening", zap.String("addr", cfg.Addr()))
		var err error
		if cfg.TLS.Enabled {
			err = relayServer.ListenAndServeTLS(cfg.TLS.Cert, cfg.TLS.Key)
		} else {
			err = relayServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			serverErr <- fmt.Errorf("relay server on %s: %w", cfg.Addr(), err)
		}
	}()

	if adminServer != nil {
		go func() {
			if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				serverErr <- fmt.Errorf("admin server on %s: %w", cfg.AdminAddr(), err)
			}
		}()
	}

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down...")
	_ = relayServer.Shutdown(context.Background())
	if adminServer != nil {
		_ = adminServer.Shutdown(context.Background())
	}
	return nil
}

// buildLogger constructs a zap.Logger based on the log config.
func buildLogger(level, format string) (*zap.Logger, error) {
	var zapLevel zap.AtomicLevel
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	var cfg zap.Config
	if format == "console" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	cfg.Level = zapLevel
	return cfg.Build()
}
