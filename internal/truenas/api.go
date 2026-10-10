package truenas

import (
	"context"
	"encoding/json"
	"time"
)

const fieldName = "name"

type Date struct {
	time.Time
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var v struct {
		Date int64 `json:"$date"`
	}
	if string(b) == "null" {
		return nil
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	d.Time = time.UnixMilli(v.Date)
	return nil
}

type Scan struct {
	Function       string   `json:"function"`
	State          string   `json:"state"`
	StartTime      *Date    `json:"start_time"`
	EndTime        *Date    `json:"end_time"`
	Percentage     *float64 `json:"percentage"`
	BytesToProcess *float64 `json:"bytes_to_process"`
	BytesProcessed *float64 `json:"bytes_processed"`
	BytesIssued    *float64 `json:"bytes_issued"`
	Errors         *float64 `json:"errors"`
	TotalSecsLeft  *float64 `json:"total_secs_left"`
}

type VdevStats struct {
	ReadErrors     float64 `json:"read_errors"`
	WriteErrors    float64 `json:"write_errors"`
	ChecksumErrors float64 `json:"checksum_errors"`
}

type Vdev struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Disk     *string   `json:"disk"`
	Stats    VdevStats `json:"stats"`
	Children []Vdev    `json:"children"`
}

type Pool struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	StatusCode string            `json:"status_code"`
	Healthy    bool              `json:"healthy"`
	Warning    bool              `json:"warning"`
	Size       *float64          `json:"size"`
	Allocated  *float64          `json:"allocated"`
	Free       *float64          `json:"free"`
	Scan       *Scan             `json:"scan"`
	Topology   map[string][]Vdev `json:"topology"`
}

type Disk struct {
	Name   string  `json:"name"`
	Serial string  `json:"serial"`
	Model  *string `json:"model"`
	Type   string  `json:"type"`
}

type SMARTTest struct {
	Num             int      `json:"num"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	Lifetime        *float64 `json:"lifetime"`
	PowerOnHoursAgo *float64 `json:"power_on_hours_ago"`
	LBAOfFirstError *float64 `json:"lba_of_first_error"`
}

type SMARTResult struct {
	Disk  string      `json:"disk"`
	Tests []SMARTTest `json:"tests"`
}

type Alert struct {
	UUID      string `json:"uuid"`
	Klass     string `json:"klass"`
	Level     string `json:"level"`
	Dismissed bool   `json:"dismissed"`
}

type reportingResult struct {
	Name   string       `json:"name"`
	Legend []string     `json:"legend"`
	Data   [][]*float64 `json:"data"`
}

func (c *Client) Pools(ctx context.Context) ([]Pool, error) {
	var out []Pool
	err := c.Call(ctx, "pool.query", []any{[]any{}, map[string]any{
		"select": []string{fieldName, "status", "status_code", "healthy", "warning", "size", "allocated", "free", "scan", "topology"},
	}}, &out)
	return out, err
}

func (c *Client) Disks(ctx context.Context) ([]Disk, error) {
	var out []Disk
	err := c.Call(ctx, "disk.query", []any{[]any{}, map[string]any{
		"select": []string{fieldName, "serial", "model", "type"},
	}}, &out)
	return out, err
}

func (c *Client) DiskTemperatures(ctx context.Context) (map[string]*float64, error) {
	var out map[string]*float64
	err := c.Call(ctx, "disk.temperatures", []any{[]any{}}, &out)
	return out, err
}

func (c *Client) SMARTResults(ctx context.Context) ([]SMARTResult, error) {
	var out []SMARTResult
	err := c.Call(ctx, "smart.test.results", []any{[]any{}}, &out)
	return out, err
}

func (c *Client) Alerts(ctx context.Context) ([]Alert, error) {
	var out []Alert
	err := c.Call(ctx, "alert.list", nil, &out)
	return out, err
}

// ARCSize returns the latest ARC size in bytes from the reporting
// database. The netdata_get_data aggregate flag must be true on 25.04:
// without it the result fails the method's own response validation.
func (c *Client) ARCSize(ctx context.Context) (float64, bool, error) {
	end := time.Now().Unix()
	var out []reportingResult
	err := c.Call(ctx, "reporting.netdata_get_data", []any{
		[]any{map[string]any{fieldName: "arcsize"}},
		map[string]any{"start": end - 300, "end": end, "aggregate": true},
	}, &out)
	if err != nil {
		return 0, false, err
	}
	for _, r := range out {
		col := -1
		for i, l := range r.Legend {
			if l == "size" {
				col = i
			}
		}
		if col < 0 {
			continue
		}
		for i := len(r.Data) - 1; i >= 0; i-- {
			if len(r.Data[i]) > col && r.Data[i][col] != nil {
				return *r.Data[i][col], true, nil
			}
		}
	}
	return 0, false, nil
}
