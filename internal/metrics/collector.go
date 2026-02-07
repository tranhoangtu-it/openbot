// Package metrics provides a lightweight, Prometheus-compatible metrics
// collector for OpenBot. It outputs text/plain in Prometheus exposition format
// without requiring the heavy prometheus/client_golang dependency.
package metrics

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Collector is the global metrics collector.
var Collector = NewMetricsCollector()

// MetricsCollector aggregates counters, gauges, and histograms.
type MetricsCollector struct {
	counters   sync.Map // name -> *Counter
	gauges     sync.Map // name -> *Gauge
	histograms sync.Map // name -> *Histogram
	startTime  time.Time
}

// NewMetricsCollector creates a new collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{startTime: time.Now()}
}

// Uptime returns how long the collector has been running.
func (c *MetricsCollector) Uptime() time.Duration {
	return time.Since(c.startTime)
}

// Counter is a monotonically increasing counter.
type Counter struct {
	name   string
	help   string
	labels string
	value  atomic.Int64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() { c.value.Add(1) }

// Add increments the counter by n.
func (c *Counter) Add(n int64) { c.value.Add(n) }

// Value returns the current counter value.
func (c *Counter) Value() int64 { return c.value.Load() }

// Gauge is a value that can go up and down.
type Gauge struct {
	name   string
	help   string
	labels string
	value  atomic.Int64
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(v int64) { g.value.Store(v) }

// Inc increments the gauge by 1.
func (g *Gauge) Inc() { g.value.Add(1) }

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() { g.value.Add(-1) }

// Value returns the current gauge value.
func (g *Gauge) Value() int64 { return g.value.Load() }

// Histogram tracks the distribution of values.
type Histogram struct {
	name    string
	help    string
	labels  string
	mu      sync.Mutex
	count   int64
	sum     float64
	buckets []histBucket
}

type histBucket struct {
	le    float64
	count int64
}

// Observe records a value in the histogram.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += v
	for i := range h.buckets {
		if v <= h.buckets[i].le {
			h.buckets[i].count++
		}
	}
}

// --- Registration helpers ---

// Counter returns or creates a counter with the given name.
func (c *MetricsCollector) Counter(name, help, labels string) *Counter {
	key := name + "{" + labels + "}"
	if v, ok := c.counters.Load(key); ok {
		return v.(*Counter)
	}
	ctr := &Counter{name: name, help: help, labels: labels}
	actual, _ := c.counters.LoadOrStore(key, ctr)
	return actual.(*Counter)
}

// Gauge returns or creates a gauge with the given name.
func (c *MetricsCollector) Gauge(name, help, labels string) *Gauge {
	key := name + "{" + labels + "}"
	if v, ok := c.gauges.Load(key); ok {
		return v.(*Gauge)
	}
	g := &Gauge{name: name, help: help, labels: labels}
	actual, _ := c.gauges.LoadOrStore(key, g)
	return actual.(*Gauge)
}

// Histogram returns or creates a histogram with the given name.
func (c *MetricsCollector) Histogram(name, help, labels string, buckets []float64) *Histogram {
	key := name + "{" + labels + "}"
	if v, ok := c.histograms.Load(key); ok {
		return v.(*Histogram)
	}
	sort.Float64s(buckets)
	hb := make([]histBucket, len(buckets))
	for i, b := range buckets {
		hb[i] = histBucket{le: b}
	}
	h := &Histogram{name: name, help: help, labels: labels, buckets: hb}
	actual, _ := c.histograms.LoadOrStore(key, h)
	return actual.(*Histogram)
}

// --- Prometheus text rendering ---

// Handler returns an http.HandlerFunc that renders metrics in Prometheus text format.
func (c *MetricsCollector) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var sb strings.Builder

		// Add uptime gauge
		fmt.Fprintf(&sb, "# HELP openbot_uptime_seconds Time since start in seconds\n")
		fmt.Fprintf(&sb, "# TYPE openbot_uptime_seconds gauge\n")
		fmt.Fprintf(&sb, "openbot_uptime_seconds %d\n\n", int64(c.Uptime().Seconds()))

		// Counters
		helpWritten := make(map[string]bool)
		c.counters.Range(func(key, value any) bool {
			ctr := value.(*Counter)
			if !helpWritten[ctr.name] {
				fmt.Fprintf(&sb, "# HELP %s %s\n", ctr.name, ctr.help)
				fmt.Fprintf(&sb, "# TYPE %s counter\n", ctr.name)
				helpWritten[ctr.name] = true
			}
			if ctr.labels != "" {
				fmt.Fprintf(&sb, "%s{%s} %d\n", ctr.name, ctr.labels, ctr.Value())
			} else {
				fmt.Fprintf(&sb, "%s %d\n", ctr.name, ctr.Value())
			}
			return true
		})

		// Gauges
		helpWritten = make(map[string]bool)
		c.gauges.Range(func(key, value any) bool {
			g := value.(*Gauge)
			if !helpWritten[g.name] {
				fmt.Fprintf(&sb, "# HELP %s %s\n", g.name, g.help)
				fmt.Fprintf(&sb, "# TYPE %s gauge\n", g.name)
				helpWritten[g.name] = true
			}
			if g.labels != "" {
				fmt.Fprintf(&sb, "%s{%s} %d\n", g.name, g.labels, g.Value())
			} else {
				fmt.Fprintf(&sb, "%s %d\n", g.name, g.Value())
			}
			return true
		})

		// Histograms
		c.histograms.Range(func(key, value any) bool {
			h := value.(*Histogram)
			h.mu.Lock()
			defer h.mu.Unlock()

			fmt.Fprintf(&sb, "# HELP %s %s\n", h.name, h.help)
			fmt.Fprintf(&sb, "# TYPE %s histogram\n", h.name)
			prefix := h.name
			if h.labels != "" {
				prefix += "{" + h.labels + ","
			} else {
				prefix += "{"
			}
			for _, b := range h.buckets {
				le := fmt.Sprintf("%g", b.le)
				if math.IsInf(b.le, 1) {
					le = "+Inf"
				}
				fmt.Fprintf(&sb, "%sle=\"%s\"} %d\n", prefix+"_bucket", le, b.count)
			}
			if h.labels != "" {
				fmt.Fprintf(&sb, "%s{%s} %d\n", h.name+"_count", h.labels, h.count)
				fmt.Fprintf(&sb, "%s{%s} %f\n", h.name+"_sum", h.labels, h.sum)
			} else {
				fmt.Fprintf(&sb, "%s_count %d\n", h.name, h.count)
				fmt.Fprintf(&sb, "%s_sum %f\n", h.name, h.sum)
			}
			return true
		})

		fmt.Fprint(w, sb.String())
	}
}

// --- Pre-defined metrics used across the application ---

var (
	MessagesTotal     = Collector.Counter("openbot_messages_total", "Total messages processed", "")
	LLMRequestsTotal  = Collector.Counter("openbot_llm_requests_total", "Total LLM API requests", "")
	ToolExecutions    = Collector.Counter("openbot_tool_executions_total", "Total tool executions", "")
	SecurityBlocks    = Collector.Counter("openbot_security_blocks_total", "Total security blocks", "")
	ActiveSessions    = Collector.Gauge("openbot_active_sessions", "Current active SSE sessions", "")
	SSEConnections    = Collector.Gauge("openbot_sse_connections", "Current SSE connections", "")

	LLMLatency = Collector.Histogram("openbot_llm_latency_seconds", "LLM request latency in seconds", "",
		[]float64{0.5, 1, 2, 5, 10, 30, 60, 120})
	ToolLatency = Collector.Histogram("openbot_tool_latency_seconds", "Tool execution latency in seconds", "",
		[]float64{0.1, 0.5, 1, 5, 10, 30})
)
