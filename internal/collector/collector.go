package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/truenas-exporter/internal/truenas"
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "truenas"

type source interface {
	Pools(ctx context.Context) ([]truenas.Pool, error)
	Disks(ctx context.Context) ([]truenas.Disk, error)
	DiskTemperatures(ctx context.Context) (map[string]*float64, error)
	SMARTResults(ctx context.Context) ([]truenas.SMARTResult, error)
	Alerts(ctx context.Context) ([]truenas.Alert, error)
	ARCSize(ctx context.Context) (float64, bool, error)
}

type group struct {
	metrics     []prometheus.Metric
	success     bool
	lastSuccess time.Time
	duration    time.Duration
}

type Collector struct {
	src           source
	interval      time.Duration
	smartInterval time.Duration

	mu     sync.RWMutex
	groups map[string]*group
}

func New(src source, interval, smartInterval time.Duration) *Collector {
	return &Collector{
		src:           src,
		interval:      interval,
		smartInterval: smartInterval,
		groups:        map[string]*group{},
	}
}

func (c *Collector) Run(ctx context.Context) {
	c.pollMain(ctx)
	fast := time.NewTicker(c.interval)
	defer fast.Stop()
	var smartC <-chan time.Time
	if c.smartInterval > 0 {
		c.pollSMART(ctx)
		smart := time.NewTicker(c.smartInterval)
		defer smart.Stop()
		smartC = smart.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-fast.C:
			c.pollMain(ctx)
		case <-smartC:
			c.pollSMART(ctx)
		}
	}
}

func (c *Collector) pollMain(ctx context.Context) {
	c.run(ctx, "disks", func(ctx context.Context) ([]prometheus.Metric, error) {
		disks, err := c.src.Disks(ctx)
		if err != nil {
			return nil, err
		}
		return diskInfoMetrics(disks), nil
	})
	c.run(ctx, "pools", func(ctx context.Context) ([]prometheus.Metric, error) {
		pools, err := c.src.Pools(ctx)
		if err != nil {
			return nil, err
		}
		return poolMetrics(pools), nil
	})
	c.run(ctx, "disk_temperatures", func(ctx context.Context) ([]prometheus.Metric, error) {
		temps, err := c.src.DiskTemperatures(ctx)
		if err != nil {
			return nil, err
		}
		return temperatureMetrics(temps), nil
	})
	c.run(ctx, "alerts", func(ctx context.Context) ([]prometheus.Metric, error) {
		alerts, err := c.src.Alerts(ctx)
		if err != nil {
			return nil, err
		}
		return alertMetrics(alerts), nil
	})
	c.run(ctx, "arc", func(ctx context.Context) ([]prometheus.Metric, error) {
		size, ok, err := c.src.ARCSize(ctx)
		if err != nil || !ok {
			return nil, err
		}
		return []prometheus.Metric{prometheus.MustNewConstMetric(arcSizeDesc, prometheus.GaugeValue, size)}, nil
	})
}

func (c *Collector) pollSMART(ctx context.Context) {
	c.run(ctx, "smart", func(ctx context.Context) ([]prometheus.Metric, error) {
		results, err := c.src.SMARTResults(ctx)
		if err != nil {
			return nil, err
		}
		return smartMetrics(results), nil
	})
}

func (c *Collector) run(ctx context.Context, name string, fn func(context.Context) ([]prometheus.Metric, error)) {
	start := time.Now()
	metrics, err := fn(ctx)
	elapsed := time.Since(start)
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.groups[name]
	if !ok {
		g = &group{}
		c.groups[name] = g
	}
	g.duration = elapsed
	if err != nil {
		slog.Error("collection failed", "collector", name, "error", err)
		g.success = false
		g.metrics = nil
		return
	}
	g.success = true
	g.lastSuccess = time.Now()
	g.metrics = metrics
	slog.Debug("collected", "collector", name, "metrics", len(metrics), "duration", elapsed)
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(c, ch)
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for name, g := range c.groups {
		ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.GaugeValue, boolFloat(g.success), name)
		ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, g.duration.Seconds(), name)
		if !g.lastSuccess.IsZero() {
			ch <- prometheus.MustNewConstMetric(collectorLastSuccessDesc, prometheus.GaugeValue, float64(g.lastSuccess.Unix()), name)
		}
		for _, m := range g.metrics {
			ch <- m
		}
	}
}

func boolFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
