package explorer

import (
	"testing"
)

func TestComputePrometheusGraphURL(t *testing.T) {
	m := Metric{
		Data: make(map[string]interface{}),
	}
	m.computePrometheusGraphURL("http://localhost:9090", "", "")
	if m.PrometheusGraphURL != "http://localhost:9090/graph" {
		t.Errorf("unexpected prometheus graph url %s", m.PrometheusGraphURL)
	}

	m.Data["__name__"] = "node_cpu_seconds_total"
	m.Data["namespace"] = "default"
	m.Data["cpu"] = "0"

	targetURL := "http://localhost:9090/graph?g0.expr=node_cpu_seconds_total%7Bnamespace%3D%22default%22%2C+cpu%3D%220%22%7D&g0.tab=0&g0.display_mode=lines&g0.show_exemplars=0&g0.range_input=10m&g0.end_input=2024-09-24+08%3A52%3A15&g0.moment_input=2024-09-24+08%3A52%3A15"

	// Two versions of target url because order in maps is nondeterministic.
	targetURL2 := "http://localhost:9090/graph?g0.expr=node_cpu_seconds_total%7Bcpu%3D%220%22%2C+namespace%3D%22default%22%7D&g0.tab=0&g0.display_mode=lines&g0.show_exemplars=0&g0.range_input=10m&g0.end_input=2024-09-24+08%3A52%3A15&g0.moment_input=2024-09-24+08%3A52%3A15"

	m.computePrometheusGraphURL("http://localhost:9090", "10m", "2024-09-24 08:52:15")
	if m.PrometheusGraphURL != targetURL && m.PrometheusGraphURL != targetURL2 {
		t.Errorf("expected %s but got %s\n", targetURL, m.PrometheusGraphURL)
	}
}
