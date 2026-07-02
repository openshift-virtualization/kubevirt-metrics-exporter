package config

import "testing"

func TestParseBoundariesNs(t *testing.T) {
	result := parseBoundariesNs("10000000,100000000,1000000000")
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != 10000000 {
		t.Errorf("result[0] = %d, want 10000000", result[0])
	}
}

func TestParseBoundariesSeconds(t *testing.T) {
	result := parseBoundariesSeconds("10000000,100000000,1000000000")
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != 0.01 {
		t.Errorf("result[0] = %f, want 0.01", result[0])
	}
	if result[1] != 0.1 {
		t.Errorf("result[1] = %f, want 0.1", result[1])
	}
	if result[2] != 1.0 {
		t.Errorf("result[2] = %f, want 1.0", result[2])
	}
}

func TestParseNamespaces(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"default", []string{"default"}},
		{"default,production", []string{"default", "production"}},
		{" default , production ", []string{"default", "production"}},
		{"ns1,,ns2", []string{"ns1", "ns2"}},
	}

	for _, tt := range tests {
		got := ParseNamespaces(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("ParseNamespaces(%q) = %v, want nil", tt.input, got)
			}
			continue
		}
		if len(got) != len(tt.want) {
			t.Errorf("ParseNamespaces(%q) len = %d, want %d", tt.input, len(got), len(tt.want))
			continue
		}
		for i, v := range got {
			if v != tt.want[i] {
				t.Errorf("ParseNamespaces(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
			}
		}
	}
}

func TestValidate(t *testing.T) {
	valid := &Config{
		NodeName:         "worker-1",
		BoundariesNs:     []int64{10000000},
		Boundaries:       []float64{0.01},
		EBPFScanInterval: 30,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config: unexpected error: %v", err)
	}

	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{"empty node name", func(c *Config) { c.NodeName = "" }},
		{"empty boundaries", func(c *Config) { c.BoundariesNs = nil }},
		{"zero scan interval with ebpf", func(c *Config) { c.EnableEBPF = true; c.EBPFScanInterval = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := *valid
			tt.modify(&c)
			if err := c.Validate(); err == nil {
				t.Error("expected validation error, got nil")
			}
		})
	}
}
