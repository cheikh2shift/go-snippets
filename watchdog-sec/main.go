package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
)

func notify(state string) {
	sent, err := daemon.SdNotify(false, state)
	if err != nil {
		log.Fatalf("SdNotify(%q) failed: %v", state, err)
	}
	log.Printf("SdNotify(%q) sent=%v", state, sent)
}

func main() {
	pingInterval := flag.Duration("ping-interval", 2*time.Second, "How often to ping the watchdog")
	hangAfter := flag.Duration("hang-after", 0, "If set, stop pinging and hang after this duration")
	normalExit := flag.Duration("normal-exit", 0, "If set, exit cleanly after this duration")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("AI Agent starting (PID=%d)", os.Getpid())
	log.Printf("WATCHDOG_USEC=%q NOTIFY_SOCKET=%q", os.Getenv("WATCHDOG_USEC"), os.Getenv("NOTIFY_SOCKET"))

	watchdog, err := daemon.SdWatchdogEnabled(false)
	if err != nil {
		log.Fatalf("Failed to check watchdog: %v", err)
	}
	if watchdog == 0 {
		log.Fatal("Watchdog not enabled — run under systemd with Type=notify + WatchdogSec set")
	}
	log.Printf("Watchdog enabled — must ping every %s", watchdog)

	// FIX 1: Type=notify services MUST send READY=1 before exiting,
	// otherwise systemd marks the unit result=protocol even on exit 0.
	// Send it immediately, before any work, and fail loudly if it errors.
	notify(daemon.SdNotifyReady)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Watchdog ping loop — tied to ctx so a "hang" can stop it.
	go func() {
		ticker := time.NewTicker(*pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Printf("Ping loop stopped (context cancelled)")
				return
			case <-ticker.C:
				notify(daemon.SdNotifyWatchdog)
			}
		}
	}()

	log.Printf("Agent doing work (ping=%s hang-after=%s normal-exit=%s)", *pingInterval, *hangAfter, *normalExit)

	// FIX 2: The old hang was a busy loop in a *separate* goroutine.
	// Go kept scheduling the ping goroutine, so pings never stopped
	// and the watchdog never fired (your journal proves this).
	// A real hang must stop the pings first, then block forever.
	if *hangAfter > 0 {
		go func() {
			time.Sleep(*hangAfter)
			log.Printf("SIMULATING HANG — stopping pings, blocking forever")
			cancel() // stop watchdog pings
			select {} // deadlock: never returns, never pings
		}()
	}

	if *normalExit > 0 {
		select {
		case sig := <-sigChan:
			log.Printf("Received %v, exiting normally", sig)
		case <-time.After(*normalExit):
			log.Printf("Normal exit after %s", *normalExit)
		}
	} else {
		sig := <-sigChan
		log.Printf("Received %v, shutting down", sig)
	}

	cancel()
	log.Printf("Agent exited cleanly")
}
