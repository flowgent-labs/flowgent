package tracing

import "time"

// OTELConfig configures the OpenTelemetry exporter.
type OTELConfig struct {
	Enabled    bool    `json:"enabled" yaml:"enabled"`
	Endpoint   string  `json:"endpoint" yaml:"endpoint"`
	Protocol   string  `json:"protocol" yaml:"protocol"`
	Timeout    int     `json:"timeout" yaml:"timeout"`
	SampleRate float64 `json:"sample_rate" yaml:"sample_rate"`
}

// MetricsConfig configures Prometheus/OTEL metrics export.
type MetricsConfig struct {
	Enabled             bool               `json:"enabled" yaml:"enabled"`
	Prometheus          bool               `json:"prometheus" yaml:"prometheus"`
	ExportInterval      time.Duration      `json:"export_interval" yaml:"export_interval"`
	HistogramBoundaries MetricsBoundaries  `json:"histogram_boundaries" yaml:"histogram_boundaries"`
	Labels              map[string]string  `json:"labels" yaml:"labels"`
}

// MetricsBoundaries defines histogram bucket boundaries.
type MetricsBoundaries struct {
	Task  []float64 `json:"task" yaml:"task"`
	LLM   []float64 `json:"llm" yaml:"llm"`
	Queue []float64 `json:"queue" yaml:"queue"`
}
