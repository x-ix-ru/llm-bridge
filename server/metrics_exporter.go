// Package server provides the HTTP server for the llm-bridge proxy,
// implementing OpenAI-compatible API endpoints and admin API.
package server

import (
	"fmt"
	"sort"
	"strings"

	"llm-bridge/backend"
	"llm-bridge/metrics"
)

// exportMetrics formats all collected vLLM and bridge metrics into
// Prometheus exposition text format.
func exportMetrics(
	allMetrics map[string]*metrics.ServerMetrics,
	pool *backend.Pool,
	bridgeSuccess int64,
	bridgeError int64,
) string {
	var buf strings.Builder

	// Collect and sort server URLs for deterministic output.
	serverURLs := make([]string, 0, len(allMetrics))
	for url := range allMetrics {
		serverURLs = append(serverURLs, url)
	}
	sort.Strings(serverURLs)

	// vLLM metrics (only if we have data).
	for _, serverURL := range serverURLs {
		sm := allMetrics[serverURL]
		if sm == nil {
			continue
		}
		writeMetric(&buf, "vllm_requests_running", "gauge",
			"Number of requests currently running on backend",
			serverURL, fmt.Sprintf("%d", sm.RequestsRunning))

		writeMetric(&buf, "vllm_requests_waiting", "gauge",
			"Number of requests waiting in queue",
			serverURL, fmt.Sprintf("%d", sm.RequestsWaiting))

		writeMetric(&buf, "vllm_kv_cache_usage_perc", "gauge",
			"KV-cache usage percentage",
			serverURL, fmt.Sprintf("%.1f", sm.KVCacheUsagePerc))

		writeMetric(&buf, "vllm_prompt_tokens_total", "counter",
			"Total prompt tokens processed",
			serverURL, fmt.Sprintf("%d", sm.PromptTokensTotal))

		writeMetric(&buf, "vllm_generation_tokens_total", "counter",
			"Total generation tokens produced",
			serverURL, fmt.Sprintf("%d", sm.GenTokensTotal))

		writeMetric(&buf, "vllm_prefill_throughput", "gauge",
			"Prefill throughput in tokens per second",
			serverURL, fmt.Sprintf("%.1f", sm.PrefillThroughput))

		writeMetric(&buf, "vllm_decode_throughput", "gauge",
			"Decode throughput in tokens per second",
			serverURL, fmt.Sprintf("%.1f", sm.DecodeThroughput))

		writeMetric(&buf, "vllm_avg_prefill_time_ms", "gauge",
			"Average prefill time per request",
			serverURL, fmt.Sprintf("%.1f", sm.AvgPrefillTimeMS))

		writeMetric(&buf, "vllm_avg_decode_time_ms", "gauge",
			"Average decode time per token",
			serverURL, fmt.Sprintf("%.1f", sm.AvgDecodeTimeMS))
	}

	// Bridge metrics — always present.
	writeMetric(&buf, "llm_bridge_requests_total", "counter",
		"Total proxy requests processed by bridge",
		"status=\"success\"", fmt.Sprintf("%d", bridgeSuccess))

	writeMetric(&buf, "llm_bridge_requests_total", "counter",
		"Total proxy requests processed by bridge",
		"status=\"error\"", fmt.Sprintf("%d", bridgeError))

	// Bridge inflight per server.
	for _, serverURL := range serverURLs {
		inflight := pool.Inflight(serverURL)
		writeMetric(&buf, "llm_bridge_inflight_requests", "gauge",
			"Current number of in-flight proxy requests",
			serverURL, fmt.Sprintf("%d", inflight))
	}

	return buf.String()
}

// writeMetric appends a single Prometheus metric block (HELP + TYPE + data)
// to the builder. The labelStr should already be formatted as
// e.g. server="http://host:8000" or status="success".
func writeMetric(buf *strings.Builder, name, mtype, help, labelStr, value string) {
	buf.WriteString("# HELP ")
	buf.WriteString(name)
	buf.WriteString(" ")
	buf.WriteString(help)
	buf.WriteString("\n# TYPE ")
	buf.WriteString(name)
	buf.WriteString(" ")
	buf.WriteString(mtype)
	buf.WriteString("\n")
	buf.WriteString(name)
	buf.WriteString("{")
	buf.WriteString(labelStr)
	buf.WriteString("}")
	buf.WriteString(" ")
	buf.WriteString(value)
	buf.WriteString("\n")
}
