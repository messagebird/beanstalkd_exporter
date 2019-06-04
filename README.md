# Beanstalkd Exporter

[![Build Status](https://travis-ci.org/messagebird/beanstalkd_exporter.svg?branch=master)](https://travis-ci.org/messagebird/beanstalkd_exporter)


Beanstalkd Exporter is a [beanstalkd](http://kr.github.io/beanstalkd/) stats exporter for [Prometheus](http://prometheus.io).

## How does it work?

Every now and then, Prometheus will request a "scrape" of metrics from
this application via an HTTP request to /metrics. During this scrape
request the exporter will connect to beanstalk and ask for stats. Stats
are fetched for the whole instance and for each individual tube.

If you have many tubes and fetching stats one-by-one takes longer than
your allowed scrape duration configured in prometheus, you can increase
the number of concurrent tube stats workers via the
`-num-tube-stat-workers` flag, to parallelize the work required.

## Usage


Running beanstalkd_exporter is as easy as executing `beanstalkd_exporter` on the command line. One argument is required: `-mapping-config` (see below for what it needs).

```bash
$ beanstalkd_exporter -config examples/servers.conf -mapping-config examples/mapping.conf
```

Use the -h flag to get help information.

```bash
$ beanstalkd_exporter -h
Usage of ./bin/beanstalkd_exporter:
  -beanstalkd.address string
    	Beanstalkd server address (default "localhost:11300")
  -log.level string
    	The log level. (default "warning")
  -mapping-config string
    	A file that describes a mapping of tube names.
  -poll int
    	The number of seconds that we poll the beanstalkd server for stats. (default 30)
  -sleep-between-tube-stats int
    	The number of milliseconds to sleep between tube stats. (default 5000)
  -num-tube-stat-workers int
    	The number of concurrent workers to use to fetch tube stats. (default 1)
  -web.listen-address string
    	Address to listen on for web interface and telemetry. (default ":8080")
  -web.telemetry-path string
    	Path under which to expose metrics. (default "/metrics")
```

## Tube name mapping

Sometimes tubes names are complicated. Sometimes tubes are dedicated to entities like users and carry on their names the user id.
But it is interesting to stat all these diffent but similar tubes together. To do this you can give beastalkd_exporter a mapping config file.

Say you have many tube names like

```
incoming-emails-7822
incoming-emails-1235
incoming-emails-8882
...
```

These tubes hold incoming emails for specific users. If you ran beanstalkd_exporter without any mapping you would get stats like this:

```
tube_current_jobs_ready{tube="incoming-emails-7822"}
tube_current_jobs_ready{tube="incoming-emails-1235"}
tube_current_jobs_ready{tube="incoming-emails-8882"}
...
```

And it would be hard to group all of them together to know things like "what is the total size of 'incoming emails' tubes".

So we create a mapping config file ("./mapping.conf") with this contents:

```
incoming-emails-(\d+)
name="incoming-emails"
user_id="$1"

some-other-tube-(\w+)-processor-(\d+)
name="some-other-tube"
processor="$1"
node_id="$2"
```

(the file format was heavily inspired by [statsd_exporter's stat mapping format](https://github.com/prometheus/statsd_exporter/blob/411b071f1f5ff3d05a2ea12be027df429bd0ca5b/mapper.go).)


Run beanstalkd_exporter with the option "-mapping-config" like this:

```bash
beanstalkd_exporter -mapping-config="./mapping.conf"
```


and the resulting stats will be like

```
tube_current_jobs_ready{tube="incoming-emails",user_id="7822"}
tube_current_jobs_ready{tube="incoming-emails",user_id="1235"}
tube_current_jobs_ready{tube="incoming-emails",user_id="8882"}
```

## License

beanstalkd_exporter is licensed under [The BSD 2-Clause License](http://opensource.org/licenses/BSD-2-Clause). Copyright (c) 2016, MessageBird

