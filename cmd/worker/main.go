package main

import (
	"context"
	"log"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("order-hub worker started")

	tickTimeout := time.NewTicker(2 * time.Second)
	tickOutbox := time.NewTicker(500 * time.Millisecond)
	defer tickTimeout.Stop()
	defer tickOutbox.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("worker stopping")
			return
		case <-tickTimeout.C:
			n, err := rt.Timeout.Tick(ctx)
			if err != nil {
				log.Printf("timeout worker: %v", err)
			} else if n > 0 {
				log.Printf("closed %d expired orders", n)
			}
		case <-tickOutbox.C:
			n, err := rt.Outbox.Tick(ctx)
			if err != nil {
				log.Printf("outbox worker: %v", err)
			} else if n > 0 {
				log.Printf("published %d events", n)
			}
		}
	}
}
