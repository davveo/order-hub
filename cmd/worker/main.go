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
	tickComp := time.NewTicker(2 * time.Second)
	tickRenew := time.NewTicker(15 * time.Second)
	tickRecon := time.NewTicker(30 * time.Second)
	defer tickTimeout.Stop()
	defer tickOutbox.Stop()
	defer tickComp.Stop()
	defer tickRenew.Stop()
	defer tickRecon.Stop()

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
		case <-tickComp.C:
			n, err := rt.Compensate.Tick(ctx)
			if err != nil {
				log.Printf("compensate worker: %v", err)
			} else if n > 0 {
				log.Printf("compensated %d tickets", n)
			}
		case <-tickRenew.C:
			n, err := rt.Renew.Tick(ctx)
			if err != nil {
				log.Printf("renew worker: %v", err)
			} else if n > 0 {
				log.Printf("renewed %d reservations", n)
			}
		case <-tickRecon.C:
			if rt.Recon == nil {
				continue
			}
			out, err := rt.Recon.Run(ctx, "", true)
			if err != nil {
				log.Printf("offer recon: %v", err)
			} else if out != nil && len(out.Diffs) > 0 {
				log.Printf("offer recon scanned %d diffs %d", out.Scanned, len(out.Diffs))
			}
		}
	}
}
