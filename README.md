# script-exporter
Prometheus exporter to invoke scripts and parse their output as metrics.

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
Build the image running: `docker build .`
