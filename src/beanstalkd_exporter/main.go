package main

import (
	"flag"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/howeyc/fsnotify"
	"github.com/kr/beanstalk"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	address           = flag.String("beanstalkd.address", "localhost:11300", "Beanstalkd server address")
	pollEvery         = flag.Int("poll", 30, "The number of seconds that we poll the beanstalkd server for stats.")
	logLevel          = flag.String("log.level", "warning", "The log level.")
	mappingConfig     = flag.String("mapping-config", "", "A file that describes a mapping of tube names.")
	sleepBetweenStats = flag.Int("sleep-between-tube-stats", 5000, "The number of milliseconds to sleep between tube stats.")
	listenAddress     = flag.String("web.listen-address", ":8080", "Address to listen on for web interface and telemetry.")
	metricsPath       = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
)

var (
	mapper    *tubeMapper
	pollMutex sync.Mutex
)

func poll(server string) {
	// only allow one polling at the time to prevent floading beanstalk with requests
	pollMutex.Lock()
	defer pollMutex.Unlock()

	// system stats
	c, err := beanstalk.Dial("tcp", server)
	if err != nil {
		log.Fatalf("Error. Can't connect to beanstalk: %v", err)
	}

	if *logLevel == "debug" {
		log.Printf("Debug: Calling %s stats()", server)
	}

	stats, err := c.Stats()
	if err != nil {
		log.Printf("Error requesting Stats(): %v", err)
		requestCount.WithLabelValues("failure", server).Inc()
		return
	}
	requestCount.WithLabelValues("success", server).Inc()

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
			ConstLabels: prometheus.Labels{"instance": server},
		})

		metric, err := prometheus.RegisterOrGet(gauge)
		if err != nil {
			log.Printf("Error calling RegisterOrGet: %v", err)
		}
		if metric != nil {
			gauge = metric.(prometheus.Gauge)
		}

		iValue, _ := strconv.ParseFloat(value, 64)
		gauge.Set(iValue)
	}

	if *logLevel == "debug" {
		log.Printf("Debug: Calling %s ListTubes()", server)
	}
	// stat every tube
	tubes, err := c.ListTubes()
	if err != nil {
		log.Printf("Error requesting ListTubes(): %v", err)
		requestCount.WithLabelValues("failure", server).Inc()
		return
	}
	requestCount.WithLabelValues("success", server).Inc()
	// for every tube
	for _, name := range tubes {
		statTube(c, server, name)
		time.Sleep(time.Duration(*sleepBetweenStats) * time.Millisecond)
	}
}

func statTube(c *beanstalk.Conn, server string, tubeName string) {
	if *logLevel == "debug" {
		log.Printf("Debug: Calling %s Tube{name: %s}.Stats()", server, tubeName)
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

	labels["instance"] = server

	// be sure all labels are set
	allLabelNames := append(mapper.getAllLabels(), "instance")
	for _, l := range allLabelNames {
		if labels[l] == "" {
			labels[l] = ""
		}
	}

	tube := beanstalk.Tube{Conn: c, Name: tubeName}
	stats, err := tube.Stats()
	if err != nil {
		log.Printf("Error tubes stats: %v", err)
		requestCount.WithLabelValues("failure", server).Inc()
		return
	}
	requestCount.WithLabelValues("success", server).Inc()

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

		metric, err := prometheus.RegisterOrGet(gaugeVec)
		if err != nil {
			log.Printf("Error calling RegisterOrGet: %v", err)
		}
		if metric != nil {
			gaugeVec = metric.(*prometheus.GaugeVec)
		}

		gauge := gaugeVec.With(labels)
		iValue, _ := strconv.ParseFloat(value, 64)
		gauge.Set(iValue)
	}

}

func watchConfig(fileName string, mapper *tubeMapper) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	err = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case ev := <-watcher.Event:
			log.Printf("Config file changed (%s), attempting reload", ev)
			err = mapper.initFromFile(fileName)
			if err != nil {
				log.Println("Error reloading config:", err)
				configLoads.WithLabelValues("failure").Inc()
			} else {
				log.Println("Config reloaded successfully")
				configLoads.WithLabelValues("success").Inc()
			}
			// Re-add the file watcher since it can get lost on some changes. E.g.
			// saving a file with vim results in a RENAME-MODIFY-DELETE event
			// sequence, after which the newly written file is no longer watched.
			err = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
		case err := <-watcher.Error:
			log.Println("Error watching config:", err)
		}
	}
}

func main() {
	flag.Parse()
	// print more info on log. like line number.
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	mapper = &tubeMapper{}
	if *mappingConfig != "" {
		err := mapper.initFromFile(*mappingConfig)
		if err != nil {
			log.Fatal("Error loading mapping config:", err)
		}
		go watchConfig(*mappingConfig, mapper)
	}

	log.Printf("Listening on port %s .", *listenAddress)
	log.Printf("Polling %s for stats every %d seconds", *address, *pollEvery)

	ticker := time.NewTicker(time.Second * time.Duration(*pollEvery))
	go func() {
		for _ = range ticker.C {
			go poll(*address)
		}
	}()

	http.Handle(*metricsPath, prometheus.Handler())
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
