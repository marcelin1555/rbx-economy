// Comando server: sobe a API de economia para jogos do Roblox.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/marcelin1555/rbx-economy/internal/api"
	"github.com/marcelin1555/rbx-economy/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("falha fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	key := os.Getenv("API_KEY")
	if len(key) < 16 {
		return errors.New("defina API_KEY com pelo menos 16 caracteres")
	}
	addr := env("ADDR", ":8080")
	dbPath := env("DB_PATH", "economy.db")
	rate, _ := strconv.Atoi(env("RATE_PER_MIN", "120"))

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := &http.Server{
		Addr:              addr,
		Handler:           api.NewHandler(st, api.Config{APIKey: key, RatePerMin: rate, MaxLeaderLen: 100}, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Encerramento limpo: termina as requisições em andamento antes de sair.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Info("servidor no ar", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("encerrando")
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
	return nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
