package main

import (
	"bosun.org/opentsdb"
	"bufio"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// serveMetricsFromText interprets text as metrics, either in Opentsdb format or Prometheus
// text exposition format.  It emits on w what it consumed, as well as meta metrics like
// script timings.  Error metrics are handled elsewhere, so that we can still return a failure
// response on w if the script fails.
func serveMetricsFromText(opentsdb bool, w http.ResponseWriter, r *http.Request, text string) error {
	reg := prometheus.NewRegistry()
	gatherers := prometheus.Gatherers{}
	var collector prometheus.Collector
	if opentsdb {
		metrics, err := translateOpenTsdb(text)
		if err != nil {
			return fmt.Errorf("Error parsing OpenTSDB text format: %v", err)
		}
		collector = &sliceCollector{metrics}
		reg.Register(collector)
		gatherers = append(gatherers, reg)
	} else {
		tp := &expfmt.TextParser{}
		nameToFam, err := tp.TextToMetricFamilies(strings.NewReader(text))
		if err != nil {
			return fmt.Errorf("Error parsing Prometheus TextFormat: %v", err)
		}
		gatherers = append(gatherers, regatherer(nameToFam))
	}

	handler := promhttp.HandlerFor(gatherers, promhttp.HandlerOpts{})
	handler.ServeHTTP(w, r)
	return nil
}

// regatherer is used to take the output from expfmt.TextParser
// and make it gatherable so that promhttp.HandlerFor can digest it.
type regatherer map[string]*dto.MetricFamily

// Gather implements Gatherer.
func (r regatherer) Gather() ([]*dto.MetricFamily, error) {
	fams := make([]*dto.MetricFamily, 0, len(r))
	for _, fam := range r {
		for i := range fam.Metric {
			sort.Sort(prometheus.LabelPairSorter(fam.Metric[i].Label))
		}
		fams = append(fams, fam)
	}
	return fams, nil
}

// sliceCollector is a prometheus.Collector based on a slice of metrics.
type sliceCollector struct {
	metrics []prometheus.Metric
}

// Collect implements Collector.
func (sc *sliceCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range sc.metrics {
		ch <- m
	}
}

// Descript implements Collector.
func (sc *sliceCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, m := range sc.metrics {
		ch <- m.Desc()
	}
}

// translateOpenTsdb takes a string containing OpenTSDB metrics
// and translates it into Prometheus metrics.
func translateOpenTsdb(input string) ([]prometheus.Metric, error) {
	scanner := bufio.NewScanner(strings.NewReader(input))
	var dpoints []opentsdb.DataPoint
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		dpoint, err := parseTcollectorValue(line)
		if err != nil {
			return []prometheus.Metric{}, err
		}
		dpoints = append(dpoints, *dpoint)
	}

	err := scanner.Err()
	if err != nil {
		return []prometheus.Metric{}, err
	}

	return dpointsToMetrics(dpoints)
}

// makeValidPromName translates OpenTSDB metric names to Prometheus metric
// names, which basically means replacing anything other than [A-Za-z_] with
// underscore.
func makeValidPromName(s string) string {
	var i int
	return strings.Map(
		func(r rune) rune {
			i++
			if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || r == '_' {
				return r
			}
			if i > 0 && '0' <= r && r <= '9' {
				return r
			}
			return '_'
		},
		s)
}

// dpoints translates OpenTSDB samples into Prometheus format.
func dpointsToMetrics(dpoints []opentsdb.DataPoint) ([]prometheus.Metric, error) {
	var metrics []prometheus.Metric

	for _, dpoint := range dpoints {
		labels := make(map[string]string, len(dpoint.Tags))
		for k, v := range dpoint.Tags {
			labels[makeValidPromName(k)] = v
		}

		var v float64
		switch x := dpoint.Value.(type) {
		case float64:
			v = x
		case int:
			v = float64(x)
		case int32:
			v = float64(x)
		case int64:
			v = float64(x)
		}

		// Although we read the timestamp into the DataPoint, I don't see a way
		// to populate the corresonding Prometheus metric with it.  That's okay for
		// this project's purpose.
		metrics = append(metrics, prometheus.MustNewConstMetric(
			prometheus.NewDesc(makeValidPromName(dpoint.Metric), "help", []string{}, labels),
			prometheus.GaugeValue, v))
	}
	return metrics, nil
}

// parseTcollectorValue parses a tcollector-style line into a data point.
// This was lifted from scollector.
func parseTcollectorValue(line string) (*opentsdb.DataPoint, error) {
	sp := strings.Fields(line)
	if len(sp) < 3 {
		return nil, fmt.Errorf("bad line: %s", line)
	}
	ts, err := strconv.ParseInt(sp[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("bad timestamp: %s", sp[1])
	}
	val, err := strconv.ParseFloat(sp[2], 64)
	if err != nil {
		return nil, fmt.Errorf("bad value: %s", sp[2])
	}
	if !opentsdb.ValidTSDBString(sp[0]) {
		return nil, fmt.Errorf("bad metric: %s", sp[0])
	}
	dp := opentsdb.DataPoint{
		Metric:    sp[0],
		Timestamp: ts,
		Value:     val,
	}
	tags := opentsdb.TagSet{}
	for _, tag := range sp[3:] {
		ts, err := opentsdb.ParseTags(tag)
		if err != nil {
			return nil, fmt.Errorf("bad tag, metric %s: %v: %v", sp[0], tag, err)
		}
		tags.Merge(ts)
	}
	dp.Tags = tags
	return &dp, nil
}
