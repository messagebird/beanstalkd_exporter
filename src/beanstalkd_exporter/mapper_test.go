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

import "testing"

func TestMetricMapper(t *testing.T) {
	scenarios := []struct {
		config    string
		configBad bool
		mappings  map[string]map[string]string
	}{
		// Empty config.
		{},
		// Config with several mapping definitions.
		{
			config: `
				some-tube-(\d*)-(\w*)
				name="some-tube"
				identifier="$1"
				action="$2"
				job="test_dispatcher"

                                another-tube-(\w*)-name
				name="another-tube-name"
				processor="$1"
				job="test_dispatcher2"
			`,
			mappings: map[string]map[string]string{
				"some-tube-773-open": map[string]string{
					"name":       "some-tube",
					"identifier": "773",
					"action":     "open",
					"job":        "test_dispatcher",
				},
				"another-tube-ImageProcessor-name": map[string]string{
					"name":      "another-tube-name",
					"processor": "ImageProcessor",
					"job":       "test_dispatcher2",
				},
			},
		},
		// Config with bad regex reference.
		{
			config: `
				test.(\w*)
				name="name"
				label="$1_foo"
			`,
			mappings: map[string]map[string]string{
				"test.a": map[string]string{
					"name":  "name",
					"label": "",
				},
			},
		},
		// Config with good regex reference.
		{
			config: `
				test.(\w*)
				name="name"
				label="${1}_foo"
			`,
			mappings: map[string]map[string]string{
				"test.a": map[string]string{
					"name":  "name",
					"label": "a_foo",
				},
			},
		},
		// Config with bad label line.
		{
			config: `
				test.(\d*).(\d*)
				name=foo
			`,
			configBad: true,
		},
		// Config with bad label line.
		{
			config: `
				test.(\d*).(\d*)
				name="foo.name"
			`,
			configBad: true,
		},
		// Config with bad tube name.
		{
			config: `
				test.(\d*).(\d*)
				name="0foo"
			`,
			configBad: true,
		},
	}

	mapper := tubeMapper{}
	for i, scenario := range scenarios {
		err := mapper.initFromString(scenario.config)
		if err != nil && !scenario.configBad {
			t.Fatalf("%d. Config load error: %s", i, err)
		}
		if err == nil && scenario.configBad {
			t.Fatalf("%d. Expected bad config, but loaded ok", i)
		}

		for tube, mapping := range scenario.mappings {
			labels, present := mapper.getMapping(tube)
			if len(labels) == 0 && present {
				t.Fatalf("%d.%q: Expected tube to not be present", i, tube)
			}
			if len(labels) != len(mapping) {
				t.Fatalf("%d.%q: Expected %d labels, got %d", i, tube, len(mapping), len(labels))
			}
			for label, value := range labels {
				if mapping[label] != value {
					t.Fatalf("%d.%q: Expected labels %v, got %v", i, tube, mapping, labels)
				}
			}
		}
	}
}
