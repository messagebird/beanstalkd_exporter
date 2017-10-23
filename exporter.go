package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kr/beanstalk"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

type Exporter struct {
	// use to protect against concurrent collection
	mutex sync.RWMutex

	address string

	nameReplacer  *regexp.Regexp
	labelReplacer *regexp.Regexp

	graceDuration time.Duration

	// scrape metrics
	scrapeCountMetric           *prometheus.CounterVec
	scrapeConnectionErrorMetric prometheus.Counter
	scrapeHistogramMetric       prometheus.Histogram

	// use to collects all the errors asynchronously
	cherrs chan error
}

func NewExporter(address string) *Exporter {
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
	}

	go func(e *Exporter) {
		for {
			log.Errorln(<-cherrs)
			e.scrapeCountMetric.WithLabelValues("failure").Inc()
		}
	}(exporter)

	return exporter
}

// Describe implements the prometheus.Collector interface, emits on the chan
// the descriptors of all the possible metrics.
// Since it's impossible to know in advance the metrics that going to be
// collected Describe is equivalent of a Collect call.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.scrape(func(c prometheus.Collector) { c.Describe(ch) })

	e.scrapeCountMetric.Describe(ch)
	e.scrapeConnectionErrorMetric.Describe(ch)
	e.scrapeHistogramMetric.Describe(ch)
	mapper.configLoadsMetric.Describe(ch)
	mapper.mappingsCountMetric.Describe(ch)
}

// Collect implements the prometheus.Collector interface, emits on the chan all
// the metrics.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.scrape(func(c prometheus.Collector) { c.Collect(ch) })

	e.scrapeCountMetric.Collect(ch)
	e.scrapeConnectionErrorMetric.Collect(ch)
	e.scrapeHistogramMetric.Collect(ch)
	mapper.configLoadsMetric.Collect(ch)
	mapper.mappingsCountMetric.Collect(ch)
}

// scrape retrieves all the available metrics and invoke the given callback on each of them.
func (e *Exporter) scrape(f func(prometheus.Collector)) {
	start := time.Now()
	defer func() {
		e.scrapeHistogramMetric.Observe(time.Since(start).Seconds())
	}()

	// system stats
	c, err := beanstalk.Dial("tcp", e.address)
	if err != nil {
		e.scrapeConnectionErrorMetric.Inc()
		log.Fatalf("Error. Can't connect to beanstalk: %v", err)
		return
	}

	if *logLevel == "debug" {
		log.Debugf("Debug: Calling %s stats()", e.address)
	}

	stats, err := c.Stats()
	if err != nil {
		log.Errorf("Error requesting Stats(): %v", err)
		e.scrapeCountMetric.WithLabelValues("failure").Inc()
		return
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

		f(gauge)
	}

	if *logLevel == "debug" {
		log.Debugf("Debug: Calling %s ListTubes()", e.address)
	}
	// stat every tube
	tubes, err := c.ListTubes()
	if err != nil {
		log.Errorf("Error requesting ListTubes(): %v", err)
		e.scrapeCountMetric.WithLabelValues("failure").Inc()
		return
	}
	e.scrapeCountMetric.WithLabelValues("success").Inc()

	// for every tube
	for _, name := range tubes {
		e.statTube(c, name, f)
		time.Sleep(time.Duration(*sleepBetweenStats) * time.Millisecond)
	}
}

func (e *Exporter) statTube(c *beanstalk.Conn, tubeName string, f func(prometheus.Collector)) {
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
		return
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

		f(gauge)
	}
}
