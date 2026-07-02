package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift-virtualization/kubevirt-storage-latency-exporter/pkg/config"
	"github.com/openshift-virtualization/kubevirt-storage-latency-exporter/pkg/device"
	bpf "github.com/openshift-virtualization/kubevirt-storage-latency-exporter/pkg/ebpf"
	"github.com/openshift-virtualization/kubevirt-storage-latency-exporter/pkg/qmp"
)

func main() {
	cfg := config.Parse()
	setupLogging(cfg.LogLevel)

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("starting kubevirt-storage-latency-exporter",
		"node", cfg.NodeName,
		"qmp", cfg.EnableQMP,
		"ebpf", cfg.EnableEBPF,
	)

	log := slog.Default()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if cfg.EnableQMP {
		startQMP(ctx, cfg, log)
	}

	if cfg.EnableEBPF {
		startEBPF(ctx, cfg, log)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{Addr: cfg.ListenAddress, Handler: mux}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("metrics server starting", "address", cfg.ListenAddress)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func startQMP(ctx context.Context, cfg *config.Config, log *slog.Logger) {
	criClient, err := qmp.NewCRIClient(cfg.QMPCRISocket)
	if err != nil {
		log.Error("qmp: creating CRI client", "error", err)
		os.Exit(1)
	}

	k8sCfg, err := rest.InClusterConfig()
	if err != nil {
		log.Error("qmp: building in-cluster config", "error", err)
		os.Exit(1)
	}

	cs, err := kubernetes.NewForConfig(k8sCfg)
	if err != nil {
		log.Error("qmp: creating clientset", "error", err)
		os.Exit(1)
	}

	collector := qmp.NewCollector(qmp.PollerConfig{
		NodeName:     cfg.NodeName,
		PollInterval: cfg.QMPPollInterval,
		BoundariesNs: cfg.BoundariesNs,
		QMPTimeout:   cfg.QMPTimeout,
		Concurrency:  cfg.QMPConcurrency,
		Namespaces:   config.ParseNamespaces(cfg.QMPNamespaces),
		LabelFilter:  cfg.QMPLabelFilter,
	}, cs, criClient, log)

	prometheus.MustRegister(collector)
	go collector.Run(ctx)

	log.Info("qmp: subsystem started")
}

func startEBPF(ctx context.Context, cfg *config.Config, log *slog.Logger) {
	resolver := device.NewResolver(
		cfg.NodeName,
		cfg.EBPFProcPath,
		time.Duration(cfg.EBPFScanInterval)*time.Second,
		log,
	)
	go resolver.Run(ctx)

	programs, err := bpf.LoadAndAttach(
		cfg.EnableEBPFBlock, cfg.EnableEBPFNFS, cfg.EnableEBPFNFSKprobe,
		cfg.EBPFBlockMapSize, cfg.EBPFNFSMapSize, cfg.EBPFNFSKprobeMapSize,
		log,
	)
	if err != nil {
		log.Warn("ebpf: failed to load programs, eBPF monitoring disabled", "error", err)
		return
	}

	go func() {
		<-ctx.Done()
		programs.Close()
	}()

	collector := bpf.NewCollector(programs, resolver, cfg.NodeName, cfg.Boundaries, log)
	prometheus.MustRegister(collector)

	log.Info("ebpf: subsystem started",
		"block", programs.BlockActive,
		"nfs", programs.NFSActive,
		"nfsKprobe", programs.NFSKprobeActive,
	)
}

func setupLogging(level string) {
	var l slog.Level
	switch strings.ToLower(level) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}
