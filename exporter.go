package main

import (
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kr/beanstalk"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

const (
	dialTimeout = 30 * time.Second
)

type Exporter struct {
	// use to protect against concurrent collection
	mutex sync.RWMutex

	conn    io.ReadWriteCloser
	address string

	connectionTimeout time.Duration

	nameReplacer  *regexp.Regexp
	labelReplacer *regexp.Regexp

	graceDuration time.Duration

	// scrape metrics
	scrapeCountMetric           *prometheus.CounterVec
	scrapeConnectionErrorMetric prometheus.Counter
	scrapeHistogramMetric       prometheus.Histogram

	// use to collects all the errors asynchronously
	cherrs chan error

	ExportTubeStats bool
}

func NewExporter(address string, exportTubeStats bool) *Exporter {
	cherrs := make(chan error)
	exporter := &Exporter{
		address: address,
		scrapeCountMetric: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "beanstalkd",
				Subsystem: "exporter",
				Name:      "requests_total",
				Help:      "The number of request to beanstalkd.",
			},
			[]string{"outcome"},
		),
		scrapeConnectionErrorMetric: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: "beanstalkd",
				Subsystem: "exporter",
				Name:      "scrape_connection_errors_total",
				Help:      "Total number of connection errors to beanstalkd.",
			},
		),
		scrapeHistogramMetric: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "beanstalkd",
				Subsystem: "exporter",
				Name:      "scrape_seconds",
				Help:      "Scrape time buckets.",
			},
		),

		cherrs: cherrs,

		ExportTubeStats: exportTubeStats,
	}

	go func(e *Exporter) {
		for {
			log.Errorln(<-cherrs)
			e.scrapeCountMetric.WithLabelValues("failure").Inc()
		}
	}(exporter)

	return exporter
}

// SetConnectionTimeout sets the connection timeout value
func (e *Exporter) SetConnectionTimeout(timeout time.Duration) {
	e.connectionTimeout = timeout
}

// Describe implements the prometheus.Collector interface, emits on the chan
// the descriptors of all the possible metrics.
// Since it's impossible to know in advance the metrics that going to be
// collected Describe is equivalent of a Collect call.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.scrapeCountMetric.Describe(ch)
	e.scrapeConnectionErrorMetric.Describe(ch)
	e.scrapeHistogramMetric.Describe(ch)
	mapper.configLoadsMetric.Describe(ch)
	mapper.mappingsCountMetric.Describe(ch)

	// TODO: move this init to the NewExporter
	// if we release a new major version.
	if e.conn == nil {
		conn, err := newLazyConn(e.address, dialTimeout, e.connectionTimeout)
		if err != nil {
			e.scrapeConnectionErrorMetric.Inc()
			log.Warnf("unable to connect to beanstalkd: %s", err)
			return
		}
		e.conn = conn
	}

	client := beanstalk.NewConn(e.conn)
	collectors := e.scrape(client)
	for _, collector := range collectors {
		collector.Describe(ch)
	}
}

// Collect implements the prometheus.Collector interface, emits on the chan all
// the metrics.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.scrapeCountMetric.Collect(ch)
	e.scrapeConnectionErrorMetric.Collect(ch)
	e.scrapeHistogramMetric.Collect(ch)
	mapper.configLoadsMetric.Collect(ch)
	mapper.mappingsCountMetric.Collect(ch)

	// TODO: move this init to the NewExporter
	// if we release a new major version.
	if e.conn == nil {
		conn, err := newLazyConn(e.address, dialTimeout, e.connectionTimeout)
		if err != nil {
			e.scrapeConnectionErrorMetric.Inc()
			log.Warnf("unable to connect to beanstalkd: %s", err)
			return
		}
		e.conn = conn
	}

	client := beanstalk.NewConn(e.conn)
	collectors := e.scrape(client)
	for _, collector := range collectors {
		collector.Collect(ch)
	}
}

// scrape retrieves all the available metrics and invoke the given callback on each of them.
func (e *Exporter) scrape(conn *beanstalk.Conn) []prometheus.Collector {
	var collectors []prometheus.Collector
	start := time.Now()
	defer func() {
		e.scrapeHistogramMetric.Observe(time.Since(start).Seconds())
	}()

	if *logLevel == "debug" {
		log.Debugf("Debug: Calling %s stats()", e.address)
	}

	stats, err := conn.Stats()
	if err != nil {
		log.Errorf("Error requesting Stats(): %v", err)
		e.scrapeCountMetric.WithLabelValues("failure").Inc()
		return collectors
	}
	e.scrapeCountMetric.WithLabelValues("success").Inc()
	for key, value := range stats {
		// ignore these stats
		if key == "hostname" || key == "id" || key == "pid" {
			continue
		}

		name := strings.Replace(key, "-", "_", -1)
		help := systemStatsHelp[key]
		if help == "" {
			help = key
		}
		gauge := prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        name,
			Help:        help,
			ConstLabels: prometheus.Labels{"instance": e.address},
		})

		iValue, _ := strconv.ParseFloat(value, 64)
		gauge.Set(iValue)
		collectors = append(collectors, gauge)
	}

	if *logLevel == "debug" {
		log.Debugf("Debug: Calling %s ListTubes()", e.address)
	}

	if !e.ExportTubeStats {
		return collectors
	}

	// stat every tube
	tubes, err := conn.ListTubes()
	if err != nil {
		log.Errorf("Error requesting ListTubes(): %v", err)
		e.scrapeCountMetric.WithLabelValues("failure").Inc()
		return collectors
	}
	e.scrapeCountMetric.WithLabelValues("success").Inc()

	var outs []<-chan []prometheus.Collector
	for i, tube := range tubes {
		out := e.scrapeWorker(i, conn, tube)
		outs = append(outs, out)
	}
	for _, out := range outs {
		tubeCollectors := <-out
		collectors = append(collectors, tubeCollectors...)
	}
	return collectors
}

func (e *Exporter) scrapeWorker(i int, c *beanstalk.Conn, name string) <-chan []prometheus.Collector {
	out := make(chan []prometheus.Collector)

	go func() {
		defer close(out)
		if *logLevel == "debug" {
			log.Debugf("Debug: scrape worker %d started", i)
		}

		if *logLevel == "debug" {
			log.Debugf("Debug: scrape worker %d fetching tube %s", i, name)
		}

		out <- e.statTube(c, name)

		if *logLevel == "debug" {
			log.Debugf("Debug: scrape worker %d finished", i)
		}
	}()
	return out
}

func (e *Exporter) statTube(c *beanstalk.Conn, tubeName string) []prometheus.Collector {
	var collectors []prometheus.Collector

	if *logLevel == "debug" {
		log.Debugf("Debug: Calling %s Tube{name: %s}.Stats()", e.address, tubeName)
	}

	var labels prometheus.Labels
	mappedLabels, mappingPresent := mapper.getMapping(tubeName)
	if mappingPresent {
		labels = mappedLabels
		labels["tube"] = labels["name"]
		delete(labels, "name")
	} else {
		labels = prometheus.Labels{"tube": tubeName}
	}

	labels["instance"] = e.address

	// be sure all labels are set
	allLabelNames := append(mapper.getAllLabels(), "instance", "tube")
	for _, l := range allLabelNames {
		if labels[l] == "" {
			labels[l] = ""
		}
	}

	tube := beanstalk.Tube{Conn: c, Name: tubeName}
	stats, err := tube.Stats()
	if err != nil {
		log.Errorf("Error tubes stats: %v", err)
		e.scrapeCountMetric.WithLabelValues("failure").Inc()
		return collectors
	}
	e.scrapeCountMetric.WithLabelValues("success").Inc()

	for key, value := range stats {
		// ignore these stats
		if key == "tube-name" || key == "name" {
			continue
		}

		name := "tube_" + strings.Replace(key, "-", "_", -1)
		help := tubeStatsHelp[key]
		if help == "" {
			help = key
		}

		gaugeVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		}, allLabelNames)

		gauge := gaugeVec.With(labels)
		iValue, _ := strconv.ParseFloat(value, 64)
		gauge.Set(iValue)
		collectors = append(collectors, gauge)
	}
	return collectors
}
