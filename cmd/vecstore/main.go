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
	"path/filepath"
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
	dataDir := flag.String("data", "", "directory for snapshots (empty disables persistence)")
	every := flag.Duration("snapshot-every", 5*time.Minute, "how often to write a snapshot")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := index.HNSWConfig{M: *m, EfConstruction: *efc, EfSearch: *efs, Seed: time.Now().UnixNano()}
	st := store.New(cfg)

	var snapPath string
	if *dataDir != "" {
		if err := os.MkdirAll(*dataDir, 0o755); err != nil {
			log.Error("create data dir", "err", err)
			os.Exit(1)
		}
		snapPath = filepath.Join(*dataDir, "vecstore.snap")
		if err := st.LoadFile(snapPath); err != nil {
			log.Error("load snapshot", "path", snapPath, "err", err)
			os.Exit(1)
		}
		log.Info("loaded snapshot", "path", snapPath, "collections", len(st.List()))
	}
	save := func() {
		if snapPath == "" {
			return
		}
		if err := st.SaveFile(snapPath); err != nil {
			log.Error("save snapshot", "err", err)
			return
		}
		log.Info("saved snapshot", "path", snapPath)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(st, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if snapPath != "" && *every > 0 {
		go func() {
			t := time.NewTicker(*every)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					save()
				}
			}
		}()
	}
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
	save()
	log.Info("stopped")
}
