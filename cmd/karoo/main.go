package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/carlosrabelo/karoo/internal/config"
	"github.com/carlosrabelo/karoo/internal/proxy"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	cfgPath := flag.String("config", "config.json", "config file path")
	idleGraceMs := flag.Int("idle_grace_ms", 15000, "milliseconds to keep upstream alive with zero clients")
	strictBroadcast := flag.Bool("strict_broadcast", false, "if true, only broadcast known mining methods (notify,set_difficulty)")
	subscribeWaitMs := flag.Int("subscribe_wait_ms", 0, "deprecated: retained for backwards compatibility")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	cfg.Compat.StrictBroadcast = *strictBroadcast
	if *subscribeWaitMs > 0 {
		log.Printf("subscribe_wait_ms is deprecated and no longer affects subscribe handling (ignored value: %dms)", *subscribeWaitMs)
	}

	px := proxy.New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ch := make(chan os.Signal, 2)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
		<-ch
		log.Printf("signal: shutting down...")
		cancel()
	}()

	go px.UpstreamManager(ctx, time.Duration(*idleGraceMs)*time.Millisecond)

	go px.VardiffLoop(ctx)
	go px.HTTPServe(ctx)
	go px.ReportLoop(ctx, 5*time.Minute)

	if err := px.AcceptLoop(ctx); err != nil {
		log.Printf("accept loop end: %v", err)
	}
	log.Printf("bye")
}
