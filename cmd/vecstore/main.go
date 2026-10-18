// Command vecstore runs the vector database HTTP server.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Amineabdessalem/vecstore/index"
	"github.com/Amineabdessalem/vecstore/server"
	"github.com/Amineabdessalem/vecstore/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	m := flag.Int("m", 16, "HNSW neighbours per node")
	efc := flag.Int("ef-construction", 200, "HNSW candidate list size while inserting")
	efs := flag.Int("ef-search", 64, "default HNSW candidate list size while searching")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := index.HNSWConfig{M: *m, EfConstruction: *efc, EfSearch: *efs, Seed: time.Now().UnixNano()}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(store.New(cfg), log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Info("listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("stopped")
}
