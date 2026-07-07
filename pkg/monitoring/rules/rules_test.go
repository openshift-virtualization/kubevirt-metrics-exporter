package rules

import (
	"os"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-virtualization/kubevirt-metrics-exporter/pkg/monitoring/rules/alerts"
)

func TestRules(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Rules Suite")
}

var _ = Describe("Alert definitions", func() {
	It("should have all required fields", func() {
		for _, a := range alerts.All() {
			Expect(a.Name).NotTo(BeEmpty(), "alert name must not be empty")
			Expect(a.Expr).NotTo(BeEmpty(), "%s: expr must not be empty", a.Name)
			Expect(a.For).NotTo(BeEmpty(), "%s: for duration must not be empty", a.Name)
			Expect(a.Labels).To(HaveKey("severity"), "%s: severity label must be set", a.Name)
			Expect(a.Annotations).To(HaveKey("summary"), "%s: summary annotation must be set", a.Name)
			Expect(a.Annotations).To(HaveKey("description"), "%s: description annotation must be set", a.Name)
			Expect(a.Annotations).To(HaveKey("runbook_url"), "%s: runbook_url annotation must be set", a.Name)
		}
	})
})

var _ = Describe("RulesAsString", func() {
	It("should produce valid YAML with expected alerts", func() {
		s, err := RulesAsString()
		Expect(err).NotTo(HaveOccurred())
		Expect(s).To(ContainSubstring("groups:"))
		Expect(s).To(ContainSubstring("CSIVolumeMultipathDegraded"))
		Expect(s).To(ContainSubstring("CSIVolumeDeviceExporterDown"))
	})
})

var _ = Describe("WritePrometheusRulesFile", func() {
	It("should write a valid rules file", func() {
		path := GinkgoT().TempDir() + "/rules.yaml"
		Expect(WritePrometheusRulesFile(path)).To(Succeed())

		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())

		content := string(data)
		wantCount := len(alerts.All())
		Expect(wantCount).To(BeNumerically(">", 0))
		Expect(strings.Count(content, "alert:")).To(Equal(wantCount))
		Expect(content).To(ContainSubstring("groups:"))
	})
})

var _ = Describe("WritePrometheusRuleManifest", func() {
	It("should write a valid PrometheusRule manifest", func() {
		path := GinkgoT().TempDir() + "/manifest.yaml"
		ns := "monitoring"
		Expect(WritePrometheusRuleManifest(path, ns)).To(Succeed())

		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())

		content := string(data)
		Expect(content).To(ContainSubstring("kind: PrometheusRule"))
		Expect(content).To(ContainSubstring("namespace: " + ns))
		wantCount := len(alerts.All())
		Expect(strings.Count(content, "alert:")).To(Equal(wantCount))
	})
})
