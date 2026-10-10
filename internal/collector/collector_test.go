package collector_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/truenas-exporter/internal/collector"
	"github.com/USA-RedDragon/truenas-exporter/internal/truenas"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

var errSMART = errors.New("smart broken")

const (
	diskB    = "sdb"
	critical = "CRITICAL"
)

type fakeSource struct {
	pools []truenas.Pool
	smart []truenas.SMARTResult
}

func (f fakeSource) Pools(context.Context) ([]truenas.Pool, error) { return f.pools, nil }
func (f fakeSource) Disks(context.Context) ([]truenas.Disk, error) {
	model := "WDC WD140EDGZ"
	return []truenas.Disk{{Name: diskB, Serial: "ABC", Model: &model, Type: "HDD"}}, nil
}
func (f fakeSource) DiskTemperatures(context.Context) (map[string]*float64, error) {
	t := 42.0
	return map[string]*float64{diskB: &t, "sdz": nil}, nil
}
func (f fakeSource) SMARTResults(context.Context) ([]truenas.SMARTResult, error) {
	if f.smart == nil {
		return nil, errSMART
	}
	return f.smart, nil
}
func (f fakeSource) Alerts(context.Context) ([]truenas.Alert, error) {
	return []truenas.Alert{
		{Klass: "VolumeStatus", Level: critical, Dismissed: true},
		{Klass: "SMART", Level: critical},
		{Klass: "SMART", Level: critical},
	}, nil
}
func (f fakeSource) ARCSize(context.Context) (float64, bool, error) { return 1024, true, nil }

func loadPools(t *testing.T) []truenas.Pool {
	t.Helper()
	b, err := os.ReadFile("testdata/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	var pools []truenas.Pool
	if err := json.Unmarshal(b, &pools); err != nil {
		t.Fatal(err)
	}
	return pools
}

func run(t *testing.T, src fakeSource, smart time.Duration) *collector.Collector {
	t.Helper()
	c := collector.New(src, time.Hour, smart)
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	return c
}

func TestCollect(t *testing.T) {
	t.Parallel()
	src := fakeSource{
		pools: loadPools(t),
		smart: []truenas.SMARTResult{
			{Disk: diskB, Tests: []truenas.SMARTTest{{Num: 1, Description: "Short offline", Status: "SUCCESS"}, {Num: 0, Description: "Extended offline", Status: "FAILED"}}},
			{Disk: "sdc", Tests: []truenas.SMARTTest{{Num: 0, Description: "Short offline", Status: "ABORTED"}}},
		},
	}
	c := run(t, src, time.Hour)
	want := `
# HELP truenas_pool_info Pool status and status code as labels, always 1.
# TYPE truenas_pool_info gauge
truenas_pool_info{pool="data",status="ONLINE",status_code="RESILVERING"} 1
# HELP truenas_pool_scan_active 1 while a scrub or resilver is running.
# TYPE truenas_pool_scan_active gauge
truenas_pool_scan_active{function="resilver",pool="data"} 1
# HELP truenas_pool_vdev_checksum_errors Checksum errors since the pool was imported or last cleared.
# TYPE truenas_pool_vdev_checksum_errors gauge
truenas_pool_vdev_checksum_errors{class="data",disk="",name="mirror-0",pool="data",type="mirror",vdev="mirror-0"} 0
truenas_pool_vdev_checksum_errors{class="data",disk="sdb",name="part-b",pool="data",type="disk",vdev="mirror-0"} 2
truenas_pool_vdev_checksum_errors{class="data",disk="sdc",name="part-c",pool="data",type="disk",vdev="mirror-0"} 0
# HELP truenas_disk_temperature_celsius Disk temperature.
# TYPE truenas_disk_temperature_celsius gauge
truenas_disk_temperature_celsius{disk="sdb"} 42
# HELP truenas_disk_smart_last_test_failed 1 when the most recent SMART self-test did not succeed and was not aborted, interrupted or still running.
# TYPE truenas_disk_smart_last_test_failed gauge
truenas_disk_smart_last_test_failed{disk="sdb",status="FAILED",test="extended_offline"} 1
truenas_disk_smart_last_test_failed{disk="sdc",status="ABORTED",test="short_offline"} 0
# HELP truenas_alerts Active TrueNAS alerts by class and level.
# TYPE truenas_alerts gauge
truenas_alerts{dismissed="false",klass="SMART",level="CRITICAL"} 2
truenas_alerts{dismissed="true",klass="VolumeStatus",level="CRITICAL"} 1
# HELP truenas_arc_size_bytes ZFS ARC size.
# TYPE truenas_arc_size_bytes gauge
truenas_arc_size_bytes 1024
`
	names := []string{
		"truenas_pool_info", "truenas_pool_scan_active", "truenas_pool_vdev_checksum_errors",
		"truenas_disk_temperature_celsius", "truenas_disk_smart_last_test_failed", "truenas_alerts", "truenas_arc_size_bytes",
	}
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), names...); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorFailureAndSMARTOff(t *testing.T) {
	t.Parallel()
	c := run(t, fakeSource{pools: loadPools(t)}, time.Hour)
	want := `
# HELP truenas_collector_success Whether the last fetch of this collector's data from TrueNAS succeeded.
# TYPE truenas_collector_success gauge
truenas_collector_success{collector="alerts"} 1
truenas_collector_success{collector="arc"} 1
truenas_collector_success{collector="disk_temperatures"} 1
truenas_collector_success{collector="disks"} 1
truenas_collector_success{collector="pools"} 1
truenas_collector_success{collector="smart"} 0
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want), "truenas_collector_success"); err != nil {
		t.Fatal(err)
	}
	off := run(t, fakeSource{pools: loadPools(t)}, 0)
	if n := testutil.CollectAndCount(off, "truenas_disk_smart_last_test_info"); n != 0 {
		t.Fatalf("SMART metrics with SMART off: %d", n)
	}
	want = strings.Replace(want, "truenas_collector_success{collector=\"smart\"} 0\n", "", 1)
	if err := testutil.CollectAndCompare(off, strings.NewReader(want), "truenas_collector_success"); err != nil {
		t.Fatal(err)
	}
}
