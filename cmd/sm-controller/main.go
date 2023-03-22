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

	"github.com/adriansr/sm-controller/internal/builder"
	"github.com/adriansr/sm-controller/internal/informer"
	"github.com/adriansr/sm-controller/internal/schema"
	"github.com/adriansr/sm-controller/internal/state"
	"github.com/adriansr/sm-controller/internal/watchers"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	coreV1 "k8s.io/api/core/v1"
	networkingV1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/adriansr/sm-controller/internal/ops"
	"github.com/adriansr/sm-controller/internal/version"
)

type options struct {
	debug          bool
	verbose        bool
	reportVersion  bool
	httpListenAddr string
	master         string
	kubeConfigPath string
	apiServer      string
	apiToken       string
}

func (o *options) newFlagSetWithDefaults(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)

	fs.BoolVar(&o.debug, "debug", false, "debug output (enables verbose)")
	fs.BoolVar(&o.verbose, "verbose", false, "verbose logging")
	fs.BoolVar(&o.reportVersion, "version", false, "report version and exit")
	fs.StringVar(&o.httpListenAddr, "listen-address", ":4054", "listen address")
	// k8s.io library refers to --master and --kubeconfig in some errors. Use the same names.
	fs.StringVar(&o.master, "master", "", "TODO")
	fs.StringVar(&o.kubeConfigPath, "kubeconfig", "", "path to kube config file")
	fs.StringVar(&o.apiServer, "server", "", "Synthetic-monitoring API server URL")
	fs.StringVar(&o.apiToken, "token", "", "Synthetic-monitoring API token")

	return fs
}

func run(output io.Writer, args []string) (finalErr error) {
	var options options

	flags := options.newFlagSetWithDefaults(filepath.Base(args[0]))
	flags.SetOutput(output)

	if stop, err := processFlags(flags, &options, args[1:]); stop || err != nil {
		return err
	}

	zl := setupLogger(flags.Name(), output, options)

	defer func() {
		logger := zl.Info()
		if finalErr != nil {
			logger = zl.Err(finalErr)
		}
		logger.Msg("Terminating.")
	}()

	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(baseCtx)

	g.Go(func() error {
		zl := zl.With().Str("subsystem", "signal handler").Logger()
		return signalHandler(ctx, &zl)
	})

	zl.Info().Msg("starting...")

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
		return runController(ctx, &zl, options.kubeConfigPath, options.apiServer, options.apiToken)
	})

	// you need to call readinessHandler.Set(true) when the application is ready
	readinessHandler.Set(true)

	err := g.Wait()

	zl.Info().Err(err).Msg("shutting down...")

	return err
}

func main() {
	output := os.Stderr
	gin.SetMode(gin.ReleaseMode)
	if err := run(output, os.Args); err != nil {
		fmt.Fprintf(output, "Error: %s\n", err.Error())
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

	if options.apiServer == "" {
		return false, errors.New("must specify a synthetic-monitoring API server (--server argument)")
	}

	if options.apiToken == "" {
		return false, errors.New("must specify a synthetic-monitoring API token (--token argument)")
	}

	return false, nil
}

func setupLogger(name string, output io.Writer, options options) zerolog.Logger {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

	zl := zerolog.New(output).With().Timestamp().Str("program", name).Logger()

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

func runController(ctx context.Context, zl *zerolog.Logger, cfgPath, apiServer, apiToken string) error {
	// This should automatically fallback to in-cluster config discovery without changes.
	config, err := clientcmd.BuildConfigFromFlags("", cfgPath)
	if err != nil {
		return fmt.Errorf("building k8s config: %w", err)
	}

	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("creating k8s clientset: %w", err)
	}

	errHandler := func(logger *zerolog.Logger) func(err error) {
		return func(err error) {
			if !errors.Is(err, watchers.ErrSkipEvent) {
				logger.Err(err).Msg("error in pipeline")
			}
		}
	}

	mainLogger := zl.With().Str("component", "generic-informer").Logger()
	svcLogger := zl.With().Str("component", "service-informer").Logger()
	ingressLogger := zl.With().Str("component", "ingress-informer").Logger()
	factory, err := informer.NewFactory(clientset,
		informer.WithResyncPeriod(time.Second*60),
		informer.WithErrorHandler(errHandler(&mainLogger)),
	)
	if err != nil {
		return fmt.Errorf("creating informer factory: %w", err)
	}

	ingressRsrc := schema.Resource{
		Group:   "networking.k8s.io",
		Version: "v1",
		Kind:    "Ingress",
		Plural:  "ingresses",
	}

	iIngress, err := factory.ForResource(ingressRsrc)
	if err != nil {
		return fmt.Errorf("creating informer for resource %s: %w", ingressRsrc, err)
	}

	C := make(chan watchers.Event, 1)

	err = iIngress.AddWatcher(
		watchers.Chain{
			watchers.TypeAssert[*networkingV1.Ingress]{},
			watchers.ResourceMetaSetter(ingressRsrc),
			watchers.Logger{Logger: &ingressLogger, Level: zerolog.DebugLevel},
			watchers.Publisher{
				C:   C,
				Ctx: ctx,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("registering watcher for %s resources: %w", ingressRsrc, err)
	}

	serviceRsrc := schema.Resource{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
		Plural:  "services",
	}

	iService, err := factory.ForResource(serviceRsrc)
	if err != nil {
		return fmt.Errorf("creating informer for resource %s: %w", serviceRsrc, err)
	}

	hasSMAnnotation := func(obj schema.Object) bool {
		notes := obj.GetAnnotations()
		_, found := notes[builder.EnabledAnnotation]
		return found
	}

	err = iService.AddWatcher(
		watchers.Chain{
			watchers.TypeAssert[*coreV1.Service]{},
			watchers.ResourceMetaSetter(serviceRsrc),
			watchers.Filter(hasSMAnnotation),
			watchers.Logger{Logger: &svcLogger, Level: zerolog.DebugLevel},
			watchers.Publisher{
				C:   C,
				Ctx: ctx,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("registering watcher for %s resources: %w", serviceRsrc, err)
	}

	defer factory.Stop() // TODO: Necessary?
	factory.Start(ctx)

	pLogger := zl.With().Str("component", "publisher").Logger()
	st := state.State{
		C:      C,
		Logger: zl.With().Str("component", "cluster-state").Logger(),
		Publisher: &state.Consolidator{
			Logger:         &pLogger,
			ApiServer:      apiServer,
			ApiToken:       apiToken,
			RequestTimeout: time.Second * 30,
		},
	}
	st.Run(ctx)
	return nil
}
