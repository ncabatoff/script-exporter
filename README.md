# script-exporter
Prometheus exporter to invoke scripts and parse their output as metrics.

[![Release](https://img.shields.io/github/release/ncabatoff/script-exporter.svg?style=flat-square")](https://github.com/ncabatoff/script-exporter/releases/latest)
[![Build Status](https://travis-ci.org/ncabatoff/script-exporter.svg?branch=master)](https://travis-ci.org/ncabatoff/script-exporter)
[![Powered By: GoReleaser](https://img.shields.io/badge/powered%20by-goreleaser-green.svg?branch=master)](https://github.com/goreleaser)

## Usage

```
script-exporter -script.path /path/to/my/scripts -web.listen-address :9661
```

Create e.g. `/path/to/my/scripts/script1`, an executable which emits on stdout metrics in the [Prometheus text exposision format](https://prometheus.io/docs/instrumenting/exposition_formats/).

Then configure your prometheus.yml to add a target like

```
  - job_name: 'script1'
    metrics_path: /metrics/script1
    static_configs:
      - targets: ['localhost:9661']
```

If you add another script, you'll need another job, because the metrics path will be different.

You also want to add a job for the script_exporter internal metrics (errors, process stats, etc) as the above job will only yield metrics emitted by script1 itself:

```
  - job_name: 'script-exporter'
    metrics_path: /metrics
    static_configs:
      - targets: ['localhost:9661']
```

## Docker
Build the image running: `docker build .`  Or just run

```
docker pull ncabatoff/script-exporter
```
