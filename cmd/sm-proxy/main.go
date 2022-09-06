package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/grafana/sm-proxy/internal/ops"
	"github.com/grafana/sm-proxy/internal/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

type options struct {
	debug          bool
	verbose        bool
	reportVersion  bool
	httpListenAddr string
}

func run(stdout io.Writer, args []string) error {
	var options options

	flags := newFlagSetWithDefaults(filepath.Base(args[0]), &options)

	// add other flags here

	if stop, err := processFlags(flags, &options, args[1:]); stop || err != nil {
		return err
	}

	zl := setupLogger(flags.Name(), stdout, options)

	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(baseCtx)

	g.Go(func() error {
		zl := zl.With().Str("subsystem", "signal handler").Logger()
		return signalHandler(ctx, &zl)
	})

	zl.Info().Msg("starting...")

	_ = ctx

	metricsRegistry := prometheus.NewRegistry()

	if err := ops.RegisterMetrics(metricsRegistry); err != nil {
		return err
	}

	readinessHandler := ops.NewReadynessHandler()

	router := ops.NewMux(&ops.MuxOpts{
		Logger:           zl.With().Str("subsystem", "opsMux").Logger(),
		PromRegisterer:   metricsRegistry,
		ReadynessHandler: readinessHandler,
		DefaultHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello, world!"))
		}),
	})

	g.Go(func() error {
		httpServer, httpListener, err := setupOpsHttpServer(ctx, g, options, &zl, router)
		if err != nil {
			zl.Error().Err(err).Msg("setting up OPS HTTP server")
			return err
		}

		return httpServer.Run(httpListener)
	})

	g.Go(func() error {
		<-ctx.Done()
		return nil
	})

	// you need to call readinessHandler.Set(true) when the application is ready
	readinessHandler.Set(true)

	err := g.Wait()

	zl.Info().Err(err).Msg("shutting down...")

	return err
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	if err := run(os.Stdout, os.Args); err != nil {
		var err2 errWithCode
		if errors.As(err, &err2) {
			os.Exit(err2.Code())
		}
		os.Exit(1)
	}
}

type errWithCode struct {
	code int
	err  error
}

func (err errWithCode) Error() string {
	return err.err.Error()
}

func (err errWithCode) Unwrap() error {
	return err.err
}

func (err errWithCode) Code() int {
	return err.code
}

func newFlagSetWithDefaults(name string, options *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)

	fs.BoolVar(&options.debug, "debug", false, "debug output (enables verbose)")
	fs.BoolVar(&options.verbose, "verbose", false, "verbose logging")
	fs.BoolVar(&options.reportVersion, "version", false, "report version and exit")
	fs.StringVar(&options.httpListenAddr, "listen-address", ":4054", "listen address")

	return fs
}

func processFlags(fs *flag.FlagSet, options *options, args []string) (stop bool, err error) {
	if err := fs.Parse(args); err != nil {
		return false, err
	}

	if options.reportVersion {
		fmt.Printf(
			"%s version=\"%s\" buildstamp=\"%s\" commit=\"%s\"\n",
			fs.Name(),
			version.Short(),
			version.Buildstamp(),
			version.Commit(),
		)
		return true, nil
	}

	if options.debug {
		options.verbose = true
	}

	return false, nil
}

func setupLogger(name string, stdout io.Writer, options options) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	zl := zerolog.New(stdout).With().Timestamp().Str("program", name).Logger()

	switch {
	case options.debug:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		zl = zl.With().Caller().Logger()

	case options.verbose:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)

	default:
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	}

	return zl
}

func signalHandler(ctx context.Context, logger *zerolog.Logger) error {
	sigCh := make(chan os.Signal, 1)

	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("returning on signal")
		return fmt.Errorf("Got signal %s", sig)

	case <-ctx.Done():
		logger.Info().Msg("returning on context done")
		return nil
	}
}

func setupOpsHttpServer(ctx context.Context, group *errgroup.Group, options options, logger *zerolog.Logger, handler http.Handler) (runner, net.Listener, error) {
	httpConfig := ops.HttpConfig{
		ListenAddr:   options.httpListenAddr,
		Logger:       logger.With().Str("subsystem", "http_ops").Logger(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	httpServer := ops.NewServer(ctx, handler, &httpConfig)

	httpListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpServer.ListenAddr())
	if err != nil {
		return nil, nil, err
	}

	group.Go(func() error {
		<-ctx.Done()
		timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer timeoutCancel()

		// we probably cannot do anything meaningful with this
		// error but return it anyways.
		return httpServer.Shutdown(timeoutCtx)
	})

	return httpServer, httpListener, nil
}

type runner interface {
	Run(l net.Listener) error
}
