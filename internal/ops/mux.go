package ops

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
)

type Mux struct {
	router         *http.ServeMux
	requestCounter *prometheus.SummaryVec
}

type MuxOpts struct {
	Logger           zerolog.Logger
	DefaultHandler   http.Handler
	ReadynessHandler *readynessHandler
	PromRegisterer   interface {
		prometheus.Registerer
		prometheus.Gatherer
	}
}

// NewMux returns an http.Handler configured to serve metrics, readiness and
// debug endpoints.
func NewMux(opts *MuxOpts) *Mux {
	router := http.NewServeMux()

	router.Handle("/", opts.DefaultHandler)

	promHandler := promhttp.InstrumentMetricHandler(
		opts.PromRegisterer,
		promhttp.HandlerFor(opts.PromRegisterer,
			promhttp.HandlerOpts{
				Registry: opts.PromRegisterer,
			}),
	)

	router.Handle("/metrics", promHandler)
	router.Handle("/ready", opts.ReadynessHandler)

	// Register pprof handlers
	router.HandleFunc("/debug/pprof/", pprof.Index)
	router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	router.HandleFunc("/debug/pprof/profile", pprof.Profile)
	router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	router.HandleFunc("/debug/pprof/trace", pprof.Trace)

	requestCounter := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: "http",
		Subsystem: "requests",
		Name:      "duration_seconds",
		Help:      "request duration",
	}, []string{
		"code",
		"method",
	})

	if err := opts.PromRegisterer.Register(requestCounter); err != nil {
		return nil
	}

	return &Mux{
		router:         router,
		requestCounter: requestCounter,
	}
}

// ServeHTTP implements http.Handler.
func (mux *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	interceptor := &codeInterceptor{ResponseWriter: w}

	start := time.Now()
	mux.router.ServeHTTP(interceptor, r)
	duration := time.Since(start).Seconds()

	mux.requestCounter.With(prometheus.Labels{
		"code":   interceptor.Code(),
		"method": r.Method,
	}).Observe(duration)
}

type codeInterceptor struct {
	http.ResponseWriter
	code int
}

func (i *codeInterceptor) WriteHeader(statusCode int) {
	i.code = statusCode
	i.ResponseWriter.WriteHeader(statusCode)
}

func (i *codeInterceptor) Code() string {
	switch i.code {
	case 0:
		return "200"

	default:
		return strconv.Itoa(i.code)
	}
}

// readynessHandler records whether the service is ready to handle requests.
//
// readyness is defined by calling the method Set(true) on the handler
// at least once. Once the ready state is set, the handler never goes
// back to the unready state.
type readynessHandler int32

// NewReadynessHandler returns a new readynessHandler set to the unready state.
func NewReadynessHandler() *readynessHandler {
	return new(readynessHandler)
}

// Set should be called once with an argument of true to indicate that
// the agent is ready to serve requests. Calling it again, no matter the
// value of the argument, has no effect.
func (h *readynessHandler) Set(v bool) {
	if v {
		atomic.StoreInt32((*int32)(h), 1)
	}
}

// ServeHTTP implements http.Handler.
func (h *readynessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt32((*int32)(h)) == 0 {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}

	// Signal readiness when the agent has connected once to the API.
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ready")
}
