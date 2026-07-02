package qmp

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var (
	virtioRegex = regexp.MustCompile(`^/machine/peripheral/(ua-(.+?)/.+)$`)
	flatRegex   = regexp.MustCompile(`^ua-(.+)$`)
)

type BlockStatsResponse struct {
	Return []BlockDevice `json:"return"`
}

type BlockDevice struct {
	QDev    string       `json:"qdev"`
	Stats   BlockStats   `json:"stats"`
	Backing *BlockDevice `json:"backing,omitempty"`
}

func (d *BlockDevice) EffectiveQDev() string {
	if d.QDev != "" {
		return d.QDev
	}
	if d.Backing != nil {
		return d.Backing.EffectiveQDev()
	}
	return ""
}

type BlockStats struct {
	RdOperations          uint64       `json:"rd_operations"`
	WrOperations          uint64       `json:"wr_operations"`
	FlushOperations       uint64       `json:"flush_operations"`
	RdTotalTimeNs         uint64       `json:"rd_total_time_ns"`
	WrTotalTimeNs         uint64       `json:"wr_total_time_ns"`
	FlushTotalTimeNs      uint64       `json:"flush_total_time_ns"`
	RdLatencyHistogram    *LatencyHist `json:"rd_latency_histogram,omitempty"`
	WrLatencyHistogram    *LatencyHist `json:"wr_latency_histogram,omitempty"`
	FlushLatencyHistogram *LatencyHist `json:"flush_latency_histogram,omitempty"`
}

type LatencyHist struct {
	Boundaries []float64 `json:"boundaries"`
	Bins       []uint64  `json:"bins"`
}

func ParseBlockStats(data []byte) (*BlockStatsResponse, error) {
	var resp BlockStatsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing blockstats: %w", err)
	}
	return &resp, nil
}

func ExtractDiskInfo(qdev string) (string, string, bool) {
	if m := virtioRegex.FindStringSubmatch(qdev); len(m) >= 3 {
		return m[2], m[1], true
	}
	if m := flatRegex.FindStringSubmatch(qdev); len(m) >= 2 {
		return m[1], qdev, true
	}
	return "", "", false
}

func HasHistograms(dev *BlockDevice) bool {
	return dev.Stats.RdLatencyHistogram != nil
}
