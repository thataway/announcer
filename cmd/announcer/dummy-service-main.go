package main

import (
	"context"
	"runtime"

	"github.com/thataway/announcer/internal/app"
	"github.com/thataway/announcer/internal/config"
	"github.com/thataway/common-lib/app/tracing/ot"
	"github.com/thataway/common-lib/logger"
	pkgNet "github.com/thataway/common-lib/pkg/net"
	"github.com/thataway/common-lib/server"
	"go.uber.org/zap"
)

func main() {
	setupContext()
	ctx := app.Context()
	logger.SetLevel(zap.InfoLevel)
	logger.Info(ctx, "--== HELLO ==--")

	confOpts := []config.Option{
		config.WithAcceptEnvironment{EnvPrefix: "ANNOUNCER"},
		config.WithSourceFile{FileName: app.ConfigFile},
		config.WithDefValue{Key: app.LoggerLevel, Val: "INFO"},
		config.WithDefValue{Key: app.MetricsEnable, Val: false},
		config.WithDefValue{Key: app.TraceEnable, Val: false},
		config.WithDefValue{Key: app.ServerGracefulShutdown, Val: "10s"},
		config.WithDefValue{Key: app.ServerEndpoint, Val: "tcp://127.0.0.1:9001"},
	}
	switch runtime.GOOS {
	case "linux":
		confOpts = append(confOpts, config.WithDefValue{Key: app.AnnounceInterfaceName, Val: "lo"})
	case "darwin":
		confOpts = append(confOpts, config.WithDefValue{Key: app.AnnounceInterfaceName, Val: "lo0"})
	}
	err := config.InitGlobalConfig(confOpts...)
	if err != nil {
		logger.Fatal(ctx, err)
	}
	if err = setupLogger(); err != nil {
		logger.Fatalf(ctx, "setup logger: %v", err)
	}
	if err = setupMetrics(); err != nil {
		logger.Fatalf(ctx, "setup metrics: %v", err)
	}
	if err = setupTracer(); err != nil {
		logger.Fatalf(ctx, "setup tracer: %v", err)
	}
	var srv *server.APIServer
	if srv, err = setupServer(ctx); err != nil {
		logger.Fatalf(ctx, "setup server: %v", err)
	}
	var endPointAddress string
	if endPointAddress, err = app.ServerEndpoint.Maybe(ctx); err != nil {
		logger.Fatalf(ctx, "get server endpoint from config: %v", err)
	}
	var ep *pkgNet.Endpoint
	if ep, err = pkgNet.ParseEndpoint(endPointAddress); err != nil {
		logger.Fatalf(ctx, "parse server endpoint (%s): %v", endPointAddress, err)
	}
	gracefulDuration, _ := app.ServerGracefulShutdown.Maybe(ctx)
	if err = srv.Run(ctx, ep, server.RunWithGracefulStop(gracefulDuration)); err != nil {
		logger.Fatalf(ctx, "run server: %v", err)
	}
	WhenHaveTracerProvider(func(tp ot.TracerProvider) {
		_ = tp.Shutdown(context.Background())
	})
	logger.Info(ctx, "--== BYE ==--")
}