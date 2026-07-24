// Command ngbs-icon-exporter is a Prometheus exporter for NGBS iCON
// heating/cooling controllers. It logs in to the controller's local web
// interface and exposes per-room and system metrics (temperature, humidity,
// dew point, setpoints, pump/valve/output state, alarms) on /metrics.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jdpx/ngbs-icon-exporter/collector"
	"github.com/jdpx/ngbs-icon-exporter/ngbs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// env returns the environment variable value or def if unset/empty.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	var (
		controller      = flag.String("controller", env("NGBS_CONTROLLER", "http://192.168.0.1"), "Base URL of the NGBS iCON controller (env NGBS_CONTROLLER).")
		sysID           = flag.String("sysid", env("NGBS_SYSID", ""), "Controller SysID / login (env NGBS_SYSID). Required.")
		password        = flag.String("password", env("NGBS_PASSWORD", ""), "Login password (env NGBS_PASSWORD). Defaults to the SysID.")
		listen          = flag.String("web.listen-address", env("NGBS_LISTEN", ":9924"), "Address to serve metrics on (env NGBS_LISTEN).")
		metricsPath     = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
		timeout         = flag.Duration("timeout", 10*time.Second, "Per-scrape timeout for talking to the controller.")
		includeInactive = flag.Bool("include-inactive", false, "Also export metrics for thermostat channels that are not installed.")
		showVersion     = flag.Bool("version", false, "Print version and exit.")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *showVersion {
		fmt.Printf("ngbs-icon-exporter %s\n", version)
		return
	}
	if *sysID == "" {
		logger.Error("no SysID configured; set --sysid or NGBS_SYSID")
		os.Exit(2)
	}

	client, err := ngbs.New(*controller, *sysID, *password, *timeout)
	if err != nil {
		logger.Error("failed to create controller client", "err", err)
		os.Exit(1)
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collector.New(client, *timeout, *includeInactive, logger),
	)

	mux := http.NewServeMux()
	mux.Handle(*metricsPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><title>NGBS iCON Exporter</title>
<h1>NGBS iCON Exporter</h1>
<p>Version %s</p>
<p><a href="%s">Metrics</a></p>
`, version, *metricsPath)
	})

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("starting ngbs-icon-exporter",
		"version", version, "listen", *listen, "controller", *controller, "path", *metricsPath)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("http server stopped", "err", err)
		os.Exit(1)
	}
}
