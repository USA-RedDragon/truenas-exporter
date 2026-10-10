package collector

import (
	"strings"

	"github.com/USA-RedDragon/truenas-exporter/internal/truenas"
	"github.com/prometheus/client_golang/prometheus"
)

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
}

//nolint:gochecknoglobals
var (
	collectorSuccessDesc     = desc("collector_success", "Whether the last fetch of this collector's data from TrueNAS succeeded.", "collector")
	collectorDurationDesc    = desc("collector_duration_seconds", "How long the last fetch of this collector's data took.", "collector")
	collectorLastSuccessDesc = desc("collector_last_success_timestamp_seconds", "Unix time of this collector's last successful fetch.", "collector")

	poolInfoDesc      = desc("pool_info", "Pool status and status code as labels, always 1.", "pool", "status", "status_code")
	poolOnlineDesc    = desc("pool_online", "1 when the pool status is ONLINE.", "pool")
	poolHealthyDesc   = desc("pool_healthy", "TrueNAS's own pool health flag.", "pool")
	poolWarningDesc   = desc("pool_warning", "TrueNAS's pool warning flag, set for example while resilvering.", "pool")
	poolSizeDesc      = desc("pool_size_bytes", "Pool size.", "pool")
	poolAllocatedDesc = desc("pool_allocated_bytes", "Pool allocated space.", "pool")
	poolFreeDesc      = desc("pool_free_bytes", "Pool free space.", "pool")

	scanActiveDesc      = desc("pool_scan_active", "1 while a scrub or resilver is running.", "pool", "function")
	scanProgressDesc    = desc("pool_scan_progress_ratio", "Progress of the running or last scan, 0 to 1.", "pool", "function")
	scanErrorsDesc      = desc("pool_scan_errors", "Errors found by the running or last scan.", "pool", "function")
	scanStartDesc       = desc("pool_scan_start_timestamp_seconds", "Start time of the running or last scan.", "pool", "function")
	scanEndDesc         = desc("pool_scan_end_timestamp_seconds", "End time of the last finished scan. ZFS keeps only the latest scan, of either function.", "pool", "function")
	scanSecondsLeftDesc = desc("pool_scan_seconds_left", "TrueNAS's estimate of the running scan's remaining time.", "pool", "function")

	vdevLabels          = []string{"pool", "class", "vdev", "name", "type", "disk"}
	vdevOnlineDesc      = desc("pool_vdev_online", "1 when the vdev or device status is ONLINE.", vdevLabels...)
	vdevStatusDesc      = desc("pool_vdev_status", "vdev or device status as a label, always 1.", append(vdevLabels, "status")...)
	vdevReadErrDesc     = desc("pool_vdev_read_errors", "Read errors since the pool was imported or last cleared.", vdevLabels...)
	vdevWriteErrDesc    = desc("pool_vdev_write_errors", "Write errors since the pool was imported or last cleared.", vdevLabels...)
	vdevChecksumErrDesc = desc("pool_vdev_checksum_errors", "Checksum errors since the pool was imported or last cleared.", vdevLabels...)

	diskInfoDesc = desc("disk_info", "Disk identity, always 1.", "disk", "serial", "model", "type")
	diskTempDesc = desc("disk_temperature_celsius", "Disk temperature.", "disk")

	smartLastTestDesc     = desc("disk_smart_last_test_info", "The disk's most recent SMART self-test, always 1.", "disk", "test", "status")
	smartLastFailedDesc   = desc("disk_smart_last_test_failed", "1 when the most recent SMART self-test did not succeed and was not aborted, interrupted or still running.", "disk", "test", "status")
	smartLastHoursAgoDesc = desc("disk_smart_last_test_power_on_hours_ago", "Power-on hours since the most recent SMART self-test.", "disk", "test")

	alertsDesc  = desc("alerts", "Active TrueNAS alerts by class and level.", "klass", "level", "dismissed")
	arcSizeDesc = desc("arc_size_bytes", "ZFS ARC size.")
)

func gauge(d *prometheus.Desc, v float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, labels...)
}

func poolMetrics(pools []truenas.Pool) []prometheus.Metric {
	var out []prometheus.Metric
	for _, p := range pools {
		out = append(out,
			gauge(poolInfoDesc, 1, p.Name, p.Status, p.StatusCode),
			gauge(poolOnlineDesc, boolFloat(p.Status == "ONLINE"), p.Name),
			gauge(poolHealthyDesc, boolFloat(p.Healthy), p.Name),
			gauge(poolWarningDesc, boolFloat(p.Warning), p.Name),
		)
		for d, v := range map[*prometheus.Desc]*float64{poolSizeDesc: p.Size, poolAllocatedDesc: p.Allocated, poolFreeDesc: p.Free} {
			if v != nil {
				out = append(out, gauge(d, *v, p.Name))
			}
		}
		out = append(out, scanMetrics(p.Name, p.Scan)...)
		for class, vdevs := range p.Topology {
			for _, v := range vdevs {
				out = append(out, vdevMetrics(p.Name, class, v.Name, v)...)
			}
		}
	}
	return out
}

func scanMetrics(pool string, s *truenas.Scan) []prometheus.Metric {
	if s == nil || s.Function == "" {
		return nil
	}
	fn := strings.ToLower(s.Function)
	active := s.State == "SCANNING"
	out := []prometheus.Metric{gauge(scanActiveDesc, boolFloat(active), pool, fn)}
	if s.Percentage != nil {
		out = append(out, gauge(scanProgressDesc, *s.Percentage/100, pool, fn))
	}
	if s.Errors != nil {
		out = append(out, gauge(scanErrorsDesc, *s.Errors, pool, fn))
	}
	if s.StartTime != nil && !s.StartTime.IsZero() {
		out = append(out, gauge(scanStartDesc, float64(s.StartTime.Unix()), pool, fn))
	}
	if s.State == "FINISHED" && s.EndTime != nil && !s.EndTime.IsZero() {
		out = append(out, gauge(scanEndDesc, float64(s.EndTime.Unix()), pool, fn))
	}
	if active && s.TotalSecsLeft != nil {
		out = append(out, gauge(scanSecondsLeftDesc, *s.TotalSecsLeft, pool, fn))
	}
	return out
}

func vdevMetrics(pool, class, top string, v truenas.Vdev) []prometheus.Metric {
	disk := ""
	if v.Disk != nil {
		disk = *v.Disk
	}
	labels := make([]string, 0, len(vdevLabels)+1)
	labels = append(labels, pool, class, top, v.Name, strings.ToLower(v.Type), disk)
	out := make([]prometheus.Metric, 0, 5)
	out = append(out,
		gauge(vdevOnlineDesc, boolFloat(v.Status == "ONLINE"), labels...),
		gauge(vdevStatusDesc, 1, append(labels, v.Status)...),
		gauge(vdevReadErrDesc, v.Stats.ReadErrors, labels...),
		gauge(vdevWriteErrDesc, v.Stats.WriteErrors, labels...),
		gauge(vdevChecksumErrDesc, v.Stats.ChecksumErrors, labels...),
	)
	for _, child := range v.Children {
		out = append(out, vdevMetrics(pool, class, top, child)...)
	}
	return out
}

func diskInfoMetrics(disks []truenas.Disk) []prometheus.Metric {
	out := make([]prometheus.Metric, 0, len(disks))
	for _, d := range disks {
		model := ""
		if d.Model != nil {
			model = *d.Model
		}
		out = append(out, gauge(diskInfoDesc, 1, d.Name, d.Serial, model, strings.ToLower(d.Type)))
	}
	return out
}

func temperatureMetrics(temps map[string]*float64) []prometheus.Metric {
	out := make([]prometheus.Metric, 0, len(temps))
	for disk, t := range temps {
		if t != nil {
			out = append(out, gauge(diskTempDesc, *t, disk))
		}
	}
	return out
}

func smartFailed(status string) bool {
	switch status {
	case "SUCCESS", "ABORTED", "INTERRUPTED", "RUNNING", "":
		return false
	default:
		return true
	}
}

func smartMetrics(results []truenas.SMARTResult) []prometheus.Metric {
	var out []prometheus.Metric
	for _, r := range results {
		if len(r.Tests) == 0 {
			continue
		}
		last := r.Tests[0]
		for _, t := range r.Tests[1:] {
			if t.Num < last.Num {
				last = t
			}
		}
		test := strings.ToLower(strings.ReplaceAll(last.Description, " ", "_"))
		out = append(out,
			gauge(smartLastTestDesc, 1, r.Disk, test, last.Status),
			gauge(smartLastFailedDesc, boolFloat(smartFailed(last.Status)), r.Disk, test, last.Status),
		)
		if last.PowerOnHoursAgo != nil {
			out = append(out, gauge(smartLastHoursAgoDesc, *last.PowerOnHoursAgo, r.Disk, test))
		}
	}
	return out
}

func alertMetrics(alerts []truenas.Alert) []prometheus.Metric {
	type key struct {
		klass, level string
		dismissed    bool
	}
	counts := map[key]float64{}
	for _, a := range alerts {
		counts[key{a.Klass, a.Level, a.Dismissed}]++
	}
	out := make([]prometheus.Metric, 0, len(counts))
	for k, n := range counts {
		dismissed := "false"
		if k.dismissed {
			dismissed = "true"
		}
		out = append(out, gauge(alertsDesc, n, k.klass, k.level, dismissed))
	}
	return out
}
