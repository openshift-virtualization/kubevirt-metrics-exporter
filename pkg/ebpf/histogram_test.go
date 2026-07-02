package ebpf

import (
	"testing"
)

var defaultBuckets = []float64{0.01, 0.1, 1}

func TestSlotsToConstHistogramEmpty(t *testing.T) {
	var slots [MaxSlots]uint64
	count, sum, buckets := SlotsToConstHistogram(slots, defaultBuckets)

	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if sum != 0 {
		t.Errorf("sum = %f, want 0", sum)
	}
	if len(buckets) != len(defaultBuckets) {
		t.Errorf("len(buckets) = %d, want %d", len(buckets), len(defaultBuckets))
	}
	for _, v := range buckets {
		if v != 0 {
			t.Errorf("expected all bucket counts to be 0, got %d", v)
		}
	}
}

func TestSlotsToConstHistogramFastOps(t *testing.T) {
	var slots [MaxSlots]uint64
	slots[5] = 100

	count, sum, buckets := SlotsToConstHistogram(slots, defaultBuckets)

	if count != 100 {
		t.Errorf("count = %d, want 100", count)
	}
	if sum <= 0 {
		t.Error("sum should be positive")
	}
	for _, b := range defaultBuckets {
		if buckets[b] != 100 {
			t.Errorf("bucket le=%.3f = %d, want 100", b, buckets[b])
		}
	}
}

func TestSlotsToConstHistogramSlowOps(t *testing.T) {
	var slots [MaxSlots]uint64
	slots[21] = 50

	count, _, buckets := SlotsToConstHistogram(slots, defaultBuckets)

	if count != 50 {
		t.Errorf("count = %d, want 50", count)
	}
	for _, b := range defaultBuckets {
		if buckets[b] != 0 {
			t.Errorf("bucket le=%.3f = %d, want 0", b, buckets[b])
		}
	}
}

func TestSlotsToConstHistogramCumulative(t *testing.T) {
	var slots [MaxSlots]uint64
	slots[5] = 10
	slots[15] = 20
	slots[19] = 30

	count, _, buckets := SlotsToConstHistogram(slots, defaultBuckets)

	if count != 60 {
		t.Errorf("count = %d, want 60", count)
	}

	expects := map[float64]uint64{
		0.01: 10,
		0.1:  30,
		1:    60,
	}
	for b, want := range expects {
		if buckets[b] != want {
			t.Errorf("bucket le=%.3f = %d, want %d", b, buckets[b], want)
		}
	}
}

func TestSlotsToConstHistogramBeyondMax(t *testing.T) {
	var slots [MaxSlots]uint64
	slots[25] = 5
	slots[10] = 10

	count, _, buckets := SlotsToConstHistogram(slots, defaultBuckets)

	if count != 15 {
		t.Errorf("count = %d, want 15", count)
	}
	if buckets[0.01] != 10 {
		t.Errorf("bucket le=0.01 = %d, want 10", buckets[0.01])
	}
	if buckets[1] != 10 {
		t.Errorf("bucket le=1 = %d, want 10", buckets[1])
	}
}

func TestCustomBuckets(t *testing.T) {
	var slots [MaxSlots]uint64
	slots[5] = 10
	slots[19] = 20
	slots[23] = 30

	custom := []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	count, _, buckets := SlotsToConstHistogram(slots, custom)

	if count != 60 {
		t.Errorf("count = %d, want 60", count)
	}

	expects := map[float64]uint64{
		0.1:  10,
		0.25: 10,
		0.5:  10,
		1:    30,
		2.5:  30,
		5:    30,
		10:   60,
		30:   60,
		60:   60,
	}
	for b, want := range expects {
		if buckets[b] != want {
			t.Errorf("bucket le=%.2f = %d, want %d", b, buckets[b], want)
		}
	}
}

func TestNamespaceAllowed(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []string
		ns         string
		want       bool
	}{
		{"empty filter allows all", nil, "anything", true},
		{"empty filter allows empty ns", nil, "", true},
		{"matching namespace allowed", []string{"default", "production"}, "default", true},
		{"non-matching namespace denied", []string{"default", "production"}, "staging", false},
		{"empty ns denied when filter set", []string{"default"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nsFilter := make(map[string]bool, len(tt.namespaces))
			for _, ns := range tt.namespaces {
				nsFilter[ns] = true
			}
			c := &Collector{namespaces: nsFilter}
			if got := c.namespaceAllowed(tt.ns); got != tt.want {
				t.Errorf("namespaceAllowed(%q) = %v, want %v", tt.ns, got, tt.want)
			}
		})
	}
}
