package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	requestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beanstalkd_exporter_requests_total",
			Help: "The number of request to beanstalkd.",
		},
		[]string{"outcome", "instance"},
	)
	configLoads = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "beanstalkd_exporter_config_reloads_total",
			Help: "The number of configuration reloads.",
		},
		[]string{"outcome"},
	)
	mappingsCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "beanstalkd_exporter_loaded_mappings_count",
		Help: "The number of configured metric mappings.",
	})
)

func init() {
	prometheus.MustRegister(requestCount)
	prometheus.MustRegister(configLoads)
	prometheus.MustRegister(mappingsCount)
}
