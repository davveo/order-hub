package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/davveo/order-hub/internal/boot"
	"github.com/davveo/order-hub/internal/conf"
)

func main() {
	rt, err := boot.Build(conf.Load())
	if err != nil {
		log.Fatal(err)
	}
	defer rt.Close()

	srv := &http.Server{
		Addr:              rt.Config.HTTPAddr,
		Handler:           rt.Engine,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       8 * time.Second,
		WriteTimeout:      8 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("order-hub api listening on %s", rt.Config.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
