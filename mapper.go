// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	identifierRE = `[a-zA-Z_-][a-zA-Z0-9_-]+`

	labelLineRE = regexp.MustCompile(`^(` + identifierRE + `)\s*=\s*"(.*)"$`)
	tubeNameRE  = regexp.MustCompile(`^` + identifierRE + `$`)
)

type tubeMapping struct {
	regex  *regexp.Regexp
	labels prometheus.Labels
}

type tubeMapper struct {
	mappings  []tubeMapping
	allLabels []string
	mutex     sync.Mutex

	configLoadsMetric   *prometheus.CounterVec
	mappingsCountMetric prometheus.Gauge
}

type configLoadStates int

const (
	searching configLoadStates = iota
	tubeDefinition
)

func newTubeMapper() *tubeMapper {
	return &tubeMapper{
		configLoadsMetric: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "beanstalkd",
				Subsystem: "exporter",
				Name:      "config_reloads_total",
				Help:      "The number of configuration reloads.",
			},
			[]string{"outcome"},
		),
		mappingsCountMetric: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "beanstalkd",
			Subsystem: "exporter",
			Name:      "loaded_mappings_count",
			Help:      "The number of configured metric mappings.",
		}),
	}
}

func (m *tubeMapper) initFromString(fileContents string) error {
	lines := strings.Split(fileContents, "\n")
	state := searching

	allLabels := map[string]int{}

	parsedMappings := []tubeMapping{}
	currentMapping := tubeMapping{labels: prometheus.Labels{}}
	for i, line := range lines {
		line := strings.TrimSpace(line)

		switch state {
		case searching:
			if line == "" {
				continue
			}
			currentMapping.regex = regexp.MustCompile("^" + line + "$")
			state = tubeDefinition

		case tubeDefinition:
			if line == "" {
				if len(currentMapping.labels) == 0 {
					return fmt.Errorf("Line %d: tube mapping didn't set any labels", i)
				}
				if _, ok := currentMapping.labels["name"]; !ok {
					return fmt.Errorf("Line %d: tube mapping didn't set a tube name", i)
				}

				parsedMappings = append(parsedMappings, currentMapping)

				state = searching
				currentMapping = tubeMapping{labels: prometheus.Labels{}}
				continue
			}

			matches := labelLineRE.FindStringSubmatch(line)
			if len(matches) != 3 {
				return fmt.Errorf("Line %d: expected label mapping line, got: %s", i, line)
			}
			label, value := matches[1], matches[2]
			if label == "name" && !tubeNameRE.MatchString(value) {
				return fmt.Errorf("Line %d: tube name '%s' doesn't match regex '%s'", i, value, tubeNameRE)
			}
			currentMapping.labels[label] = value
			allLabels[label] = 1
		default:
			panic("illegal state")
		}
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.mappings = parsedMappings

	// get the list of unique labels across all mappings
	delete(allLabels, "name")
	allLabels["tube"] = 1
	labelNames := make([]string, len(allLabels))
	i := 0
	for k := range allLabels {
		labelNames[i] = k
		i++
	}
	m.allLabels = labelNames

	m.mappingsCountMetric.Set(float64(len(parsedMappings)))

	return nil
}

func (m *tubeMapper) initFromFile(fileName string) error {
	mappingStr, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	return m.initFromString(string(mappingStr))
}

func (m *tubeMapper) getMapping(originalTube string) (labels prometheus.Labels, present bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, mapping := range m.mappings {
		matches := mapping.regex.FindStringSubmatchIndex(originalTube)
		if len(matches) == 0 {
			continue
		}

		labels := prometheus.Labels{}
		for label, valueExpr := range mapping.labels {
			value := mapping.regex.ExpandString([]byte{}, valueExpr, originalTube, matches)
			labels[label] = string(value)
		}
		return labels, true
	}

	return nil, false
}

func (m *tubeMapper) getAllLabels() []string {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	return m.allLabels
}
