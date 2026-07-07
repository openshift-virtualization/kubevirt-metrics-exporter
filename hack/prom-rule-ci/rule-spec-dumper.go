package main

import (
	"fmt"
	"os"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/pkg/monitoring/rules"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: rule-spec-dumper <output-file>\n")
		os.Exit(1)
	}
	if err := rules.WritePrometheusRulesFile(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "rule-spec-dumper: %v\n", err)
		os.Exit(1)
	}
}
