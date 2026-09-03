package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	chicors "github.com/go-chi/cors"

	"dynamic-pdb/backend/internal/auth"
	"dynamic-pdb/backend/internal/config"
	"dynamic-pdb/backend/internal/db"
	"dynamic-pdb/backend/internal/httpapi"
	"dynamic-pdb/backend/internal/integrations/github"
	"dynamic-pdb/backend/internal/integrations/s3"
	"dynamic-pdb/backend/internal/services/cdn"
)

func main() {
	os.Exit(run())
}

func run() int {
	env := os.Getenv("DYNAMIC_PDB_ENV")
	if env == "" {
		env = "local"
	}
	slog.Info("loading config")

	cfg, err := config.ReadFromFile(env)
	if err != nil {
		slog.Error("read config failed", "err", err)
		return 1
	}

	database, err := db.NewDB(cfg.DB)
	if err != nil {
		slog.Error("connect db failed", "err", err)
		return 1
	}
	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("close db failed", "err", err)
		}
	}()

	jwt := auth.NewJWT(cfg.Auth.JWT.Secret, cfg.Auth.JWT.Issuer, cfg.Auth.JWT.TTL)
	githubClient := github.NewClient(
		github.WithOAuthClientID(cfg.Auth.GitHub.ClientID),
		github.WithOAuthClientSecret(cfg.Auth.GitHub.ClientSecret),
	)
	fileUploadBucket, err := s3.NewBucket(context.Background(), cfg.CDN.S3)
	if err != nil {
		slog.Error("file upload bucket init failed", "err", err)
		return 1
	}
	fileCDN, err := cdn.NewService(fileUploadBucket, cfg.CDN)
	if err != nil {
		slog.Error("file CDN init failed", "err", err)
		return 1
	}
	srv := httpapi.NewServer(githubClient, fileCDN, cfg.Auth, jwt, database)

	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(corsMiddleware(env))
	router.Use(httpapi.GlobalRateLimitMiddleware(env))
	router.Use(httpapi.MediaTypeMiddleware())
	router.Use(httpapi.RequestBodyLimitMiddleware(httpapi.MaxRequestBodyBytes))
	httpapi.HandlerWithOptions(srv, httpapi.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: httpapi.RouteErrorHandler,
		Middlewares: []httpapi.MiddlewareFunc{
			httpapi.AuthMiddleware(jwt, database),
		},
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := strings.TrimSpace(cfg.Server.Addr)
	if addr == "" {
		addr = ":8080"
	}
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       3 * time.Minute,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	slog.Info("server listening", "addr", httpServer.Addr)
	if err := serve(ctx, httpServer); err != nil {
		slog.Error("server exited with error", "err", err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, httpServer *http.Server) error {
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	}
}

func corsMiddleware(env string) func(http.Handler) http.Handler {
	allowedOrigins := []string{
		"https://dynamicpdb.com",
		"https://unrevealable-fleshily-brigitte.ngrok-free.dev",
	}
	if env == "local" {
		allowedOrigins = append(
			allowedOrigins,
			"http://localhost:3000",
			"http://127.0.0.1:3000",
			"https://*.ngrok-free.dev",
			"https://*.ngrok-free.app",
			"https://*.ngrok.io",
			"https://*.ngrok.app",
		)
	}
	return chicors.Handler(chicors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodPatch,
			http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Authorization",
			"Content-Type",
			"Accept",
		},
		MaxAge: 300,
	})
}
