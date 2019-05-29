package main

import (
	"flag"
	"net/http"

	"github.com/howeyc/fsnotify"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var (
	address            = flag.String("beanstalkd.address", "localhost:11300", "Beanstalkd server address")
	logLevel           = flag.String("log.level", "warning", "The log level.")
	mappingConfig      = flag.String("mapping-config", "", "A file that describes a mapping of tube names.")
	sleepBetweenStats  = flag.Int("sleep-between-tube-stats", 5000, "The number of milliseconds to sleep between tube stats.")
	numTubeStatWorkers = flag.Int("num-tube-stat-workers", 1, "The number of concurrent workers to use to fetch tube stats.")
	listenAddress      = flag.String("web.listen-address", ":8080", "Address to listen on for web interface and telemetry.")
	metricsPath        = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
)

var (
	mapper   *tubeMapper
	registry *prometheus.Registry
)

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
			log.Warnf("Config file changed (%s), attempting reload", ev)
			err = mapper.initFromFile(fileName)
			if err != nil {
				log.Errorf("Error reloading config: %v", err)
				mapper.configLoadsMetric.WithLabelValues("failure").Inc()
			} else {
				log.Warn("Config reloaded successfully")
				mapper.configLoadsMetric.WithLabelValues("success").Inc()
			}
			// Re-add the file watcher since it can get lost on some changes. E.g.
			// saving a file with vim results in a RENAME-MODIFY-DELETE event
			// sequence, after which the newly written file is no longer watched.
			err = watcher.WatchFlags(fileName, fsnotify.FSN_MODIFY)
		case err := <-watcher.Error:
			log.Errorf("Error watching config: %v", err)
		}
	}
}

func main() {
	flag.Parse()

	if *logLevel == "debug" {
		log.SetLevel(log.DebugLevel)
	}

	mapper = newTubeMapper()
	if *mappingConfig != "" {
		err := mapper.initFromFile(*mappingConfig)
		if err != nil {
			log.Fatal("Error loading mapping config:", err)
		}
		go watchConfig(*mappingConfig, mapper)
	}

	registry = prometheus.NewRegistry()
	registry.MustRegister(NewExporter(*address))

	http.Handle(*metricsPath, promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError},
	))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`
			<html>
              <head><title>Beanstalkd Exporter</title></head>
              <body>
                <h1>Beanstalkd Exporter</h1>
                <p><a href='` + *metricsPath + `'>Metrics</a></p>
              </body>
            </html>
		`),
		)
	})

	log.Warnf("Listening on port %s .", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
