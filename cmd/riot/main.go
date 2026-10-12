// Command riot runs RIOT, Roller derby Insights On Tournaments: a
// website that takes CRG scoreboard game files and statsbooks, grouped in
// namespaces by tournament, and shows tournament stats, officiating crews
// and what the statsbooks corrected.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"riot/internal/logs"
	"riot/internal/store"
	"riot/internal/web"
)

// version is set by the build.
var version = "dev"

func main() {
	addr := flag.String("addr", cmp.Or(os.Getenv("RIOT_ADDR"), ":4201"), "address to listen on (RIOT_ADDR)")
	data := flag.String("data", cmp.Or(os.Getenv("RIOT_DATA"), "data"), "folder to keep namespaces in (RIOT_DATA)")
	logDir := flag.String("logs", cmp.Or(os.Getenv("RIOT_LOGS"), "logs"), "log folder: riot.log, and <id>.log per namespace (RIOT_LOGS)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	health := flag.Bool("health", false, "check that the riot on -addr answers, exit 0 if so (the container's health check)")
	flag.Parse()
	if *showVersion {
		fmt.Println("riot", version)
		return
	}
	if *health {
		host, port, _ := net.SplitHostPort(*addr)
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("http://" + net.JoinHostPort(cmp.Or(host, "127.0.0.1"), port) + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	lg, err := logs.Open(*logDir, os.Stderr)
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(*data)
	if err != nil {
		log.Fatal(err)
	}
	site, err := web.New(web.Config{Store: st, Logs: lg, Version: version})
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Addr: *addr, Handler: site.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 10 * time.Minute, WriteTimeout: 10 * time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st.Clean()
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	lg.Printf("", "riot %s: listening on %s; %d namespaces in %q, logs in %q", version, *addr, st.Count(), *data, *logDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
