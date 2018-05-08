// +build go1.8

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	mDuration = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_duration_seconds_total",
		Help: "time elapsed executing script",
	}, []string{"script_name"})
	mConcExceeds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_concurrency_exceeds_total",
		Help: "number of times script was not executed because there were already too many executions ongoing",
	}, []string{"script_name"})
	mRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_runs_total",
		Help: "number of times script execution attempted",
	}, []string{"script_name"})
	mErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_errors_total",
		Help: "number of script executions that ended with an error",
	}, []string{"script_name"})
	mParseErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_parse_errors_total",
		Help: "number of script executions that ended without error but produced unparseable output",
	}, []string{"script_name"})
	mTimeouts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "script_timeouts_total",
		Help: "number of script executions that were killed due to timeout",
	}, []string{"script_name"})
	mRunning = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "script_running",
		Help: "number of executions ongoing",
	}, []string{"script_name"})
)

func init() {
	prometheus.MustRegister(mDuration)
	prometheus.MustRegister(mConcExceeds)
	prometheus.MustRegister(mRuns)
	prometheus.MustRegister(mErrors)
	prometheus.MustRegister(mParseErrors)
	prometheus.MustRegister(mTimeouts)
	prometheus.MustRegister(mRunning)
}

// A runresult describes the result of executing a script.
type runresult struct {
	// Stdout of script invocation.  Any stderr output results in an error.
	output string
	// Error resulting from script invocation, or nil.
	err error
}

// A runreq is a request to run a script and capture its output
type runreq struct {
	// Context to run in (allows for cancelling requests)
	ctx context.Context

	// Script to run, relative to scriptPath.
	script string

	// Result of running script.
	result chan runresult
}

// ScriptHandler is the core of this app.
type ScriptHandler struct {
	// if opentsdb is true, interpret script output as opentsdb text format instead of Prometheus'
	opentsdb bool

	// Prefix of request path to strip off
	metricsPath string

	// Filesystem path prefix to prepend to all scripts.
	scriptPath string

	// Used internally to manage concurrency.
	reqchan chan runreq

	// Max number of concurrent requests per script
	scriptWorkers int

	// Max duration of any script invocation
	timeout time.Duration

	// mtx must be locked before modifying any fields below it (preceding
	// fields are not supposed to be modifyied.)
	mtx sync.Mutex

	// Count of running script invocations by script name.
	numChildren map[string]int
}

func NewScriptHandler(metricsPath, scriptPath string, opentsdb bool, scriptWorkers int, timeout time.Duration) *ScriptHandler {
	return &ScriptHandler{
		metricsPath:   metricsPath,
		scriptPath:    scriptPath,
		opentsdb:      opentsdb,
		numChildren:   make(map[string]int),
		reqchan:       make(chan runreq),
		scriptWorkers: scriptWorkers,
		timeout:       timeout,
	}
}

// ServeHTTP implements http.Handler.  It handles incoming HTTP requests by
// stripping off the metricsPath prefix, executing scriptPath + the remaining
// script name, interpreting the output as metrics, then publishing the result
// as a regular Prometheus metrics response.
func (sh *ScriptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	script := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, sh.metricsPath), "/")
	if script == "" {
		promhttp.Handler().ServeHTTP(w, r)
	} else {
		reschan := make(chan runresult)
		ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(sh.timeout))
		defer cancel()
		sh.reqchan <- runreq{script: script, result: reschan, ctx: ctx}
		result := <-reschan

		if result.err != nil {
			log.Printf("error running script '%s': %v", script, result.err)
		} else if err := serveMetricsFromText(sh.opentsdb, w, r, result.output); err != nil {
			log.Printf("error parsing output from script '%s': %v", script, err)
			mParseErrors.WithLabelValues(script).Add(1)
		}
	}
}

// Start will run forever, handling incoming runreqs.
func (sh *ScriptHandler) Start() {
	for req := range sh.reqchan {
		sh.mtx.Lock()
		curChildCount := sh.numChildren[req.script]
		sh.mtx.Unlock()

		if curChildCount >= sh.scriptWorkers {
			mConcExceeds.WithLabelValues(req.script).Add(1)
			err := fmt.Errorf("can't spawn a new instance of script '%s': already have %d running", req.script, curChildCount)
			req.result <- runresult{err: err}
			continue
		}

		sh.mtx.Lock()
		sh.numChildren[req.script]++
		sh.mtx.Unlock()

		mRunning.WithLabelValues(req.script).Add(1)

		go func(req runreq) {
			mRuns.WithLabelValues(req.script).Add(1)
			start := time.Now()
			ctx, cancel := context.WithCancel(req.ctx)
			defer cancel()

			output, err := runCommand(ctx, path.Join(sh.scriptPath, req.script))
			elapsed := time.Since(start)
			mDuration.WithLabelValues(req.script).Add(float64(elapsed) / float64(time.Second))

			if err != nil {
				mErrors.WithLabelValues(req.script).Add(1)
			}
			if err == context.DeadlineExceeded {
				mTimeouts.WithLabelValues(req.script).Add(1)
			}

			sh.mtx.Lock()
			sh.numChildren[req.script]--
			sh.mtx.Unlock()
			mRunning.WithLabelValues(req.script).Add(-1)

			req.result <- runresult{output: output, err: err}
		}(req)
	}
}

func main() {
	var (
		listenAddress = flag.String("web.listen-address", ":9661",
			"Address on which to expose metrics and web interface.")
		metricsPath = flag.String("web.telemetry-path", "/metrics",
			"Path under which to expose metrics.")
		scriptPath = flag.String("script.path", "",
			"path under which scripts are located")
		opentsdb = flag.Bool("opentsdb", false,
			"expect opentsdb-format metrics from script output")
		timeout = flag.Duration("timeout", time.Minute,
			"how long a script can run before being cancelled")
		scworkers = flag.Int("script-workers", 1,
			"allow this many concurrent requests per script")
	)
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
			<head><title>Script Exporter</title></head>
			<body>
			<h1>Script Exporter</h1>
			<p><a href="` + *metricsPath + `">Metrics</a></p>
			</body>
			</html>`))
	})

	sh := NewScriptHandler(*metricsPath, *scriptPath, *opentsdb, *scworkers, *timeout)
	go sh.Start()
	http.Handle(*metricsPath+"/", sh)
	http.Handle(*metricsPath, promhttp.Handler())

	srv := &http.Server{Addr: *listenAddress, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Unable to setup HTTP server: %v", err)
	}
}
