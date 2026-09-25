package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AhsokaTano26/tanoimg/internal/app"
)

var version = "0.1.0"

func main() {
	command := "serve"
	if len(os.Args) > 1 && (os.Args[1] == "serve" || os.Args[1] == "migrate") {
		command = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	data := flag.String("data", env("TANOIMG_DATA", "data"), "data directory")
	addr := flag.String("addr", env("TANOIMG_ADDR", ":3000"), "listen address")
	from := flag.String("from", "", "EasyImg directory containing db/ and uploads/")
	flag.Parse()
	if command == "migrate" {
		if *from == "" {
			log.Fatal("migrate requires -from <easyimg directory>")
		}
		a, err := app.New(app.Config{DataDir: *data})
		if err != nil {
			log.Fatal(err)
		}
		defer a.Close()
		report, err := a.MigrateEasyImg(*from)
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		return
	}
	a, err := app.New(app.Config{DataDir: *data, Version: version, InitializeAdmin: true, AdminUsername: env("TANOIMG_ADMIN_USER", "admin"), AdminPassword: os.Getenv("TANOIMG_ADMIN_PASSWORD"), TrustProxy: os.Getenv("TANOIMG_TRUST_PROXY") == "true", PublicURL: os.Getenv("TANOIMG_PUBLIC_URL")})
	if err != nil {
		log.Fatal(err)
	}
	a.StartModeration()
	a.StartNotifications()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	retentionDone := a.StartRetention(ctx)
	server := &http.Server{Addr: *addr, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP shutdown: %v", err)
		}
	}()
	log.Printf("TanoImg listening on %s; data=%s", *addr, *data)
	serveErr := server.ListenAndServe()
	stop()
	<-shutdownDone
	<-retentionDone
	if err := a.Close(); err != nil {
		log.Printf("close storage: %v", err)
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		log.Fatal(serveErr)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
