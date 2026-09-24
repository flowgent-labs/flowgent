// Package trace adapts observability backends to Flowgent's stable trace model.
package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

const maxJaegerResponseBytes = 16 << 20

// RunTraceQuery is the narrow backend contract required by the HTTP handler.
type RunTraceQuery interface {
	QueryRun(ctx context.Context, runID string) (*entities.RunTrace, error)
}

// JaegerClient queries the Jaeger Query API and normalizes its storage-specific
// JSON into Flowgent's OpenTelemetry-oriented domain model.
type JaegerClient struct {
	baseURL  *url.URL
	client   *http.Client
	service  string
	lookback string
	limit    int
}

// NewJaegerClient creates a bounded Jaeger Query API client.
func NewJaegerClient(endpoint string, timeout time.Duration, lookback string, limit int) (*JaegerClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse Jaeger query endpoint: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("Jaeger query endpoint must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("Jaeger query endpoint host is required")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if lookback == "" {
		lookback = "24h"
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return &JaegerClient{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: timeout},
		service:  "flowgent-jobmanager",
		lookback: lookback,
		limit:    limit,
	}, nil
}

func (c *JaegerClient) QueryRun(ctx context.Context, runID string) (*entities.RunTrace, error) {
	queryURL := *c.baseURL
	queryURL.Path = strings.TrimRight(queryURL.Path, "/") + "/api/traces"
	queryURL.RawQuery = ""
	tags, _ := json.Marshal(map[string]string{"run.id": runID})
	params := queryURL.Query()
	params.Set("service", c.service)
	params.Set("lookback", c.lookback)
	params.Set("limit", strconv.Itoa(c.limit))
	params.Set("tags", string(tags))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Jaeger trace request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Jaeger traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("query Jaeger traces: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload jaegerResponse
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxJaegerResponseBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode Jaeger traces: %w", err)
	}

	result := &entities.RunTrace{
		RunID:     runID,
		Source:    "jaeger",
		FetchedAt: time.Now().UTC(),
		Traces:    make([]entities.TraceInfo, 0, len(payload.Data)),
	}
	for _, raw := range payload.Data {
		traceInfo, matches := normalizeTrace(raw, runID)
		if matches {
			result.Traces = append(result.Traces, traceInfo)
		}
	}
	sort.Slice(result.Traces, func(i, j int) bool {
		return result.Traces[i].StartTime.Before(result.Traces[j].StartTime)
	})
	return result, nil
}

type jaegerResponse struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

type jaegerSpan struct {
	TraceID       string            `json:"traceID"`
	SpanID        string            `json:"spanID"`
	OperationName string            `json:"operationName"`
	References    []jaegerReference `json:"references"`
	StartTime     int64             `json:"startTime"`
	Duration      int64             `json:"duration"`
	Tags          []jaegerTag       `json:"tags"`
	Logs          []jaegerLog       `json:"logs"`
	ProcessID     string            `json:"processID"`
	Warnings      []string          `json:"warnings"`
}

type jaegerReference struct {
	RefType string `json:"refType"`
	TraceID string `json:"traceID"`
	SpanID  string `json:"spanID"`
}

type jaegerTag struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type jaegerLog struct {
	Timestamp int64       `json:"timestamp"`
	Fields    []jaegerTag `json:"fields"`
}

type jaegerProcess struct {
	ServiceName string      `json:"serviceName"`
	Tags        []jaegerTag `json:"tags"`
}

func normalizeTrace(raw jaegerTrace, runID string) (entities.TraceInfo, bool) {
	traceInfo := entities.TraceInfo{
		TraceID:  raw.TraceID,
		Services: []string{},
		Spans:    make([]entities.TraceSpan, 0, len(raw.Spans)),
	}
	serviceSet := make(map[string]struct{})
	var earliest, latest time.Time
	matches := false
	for _, rawSpan := range raw.Spans {
		attributes := tagsToMap(rawSpan.Tags)
		if fmt.Sprint(attributes["run.id"]) == runID || fmt.Sprint(attributes["flowgent.run_id"]) == runID {
			matches = true
		}
		process := raw.Processes[rawSpan.ProcessID]
		if process.ServiceName != "" {
			serviceSet[process.ServiceName] = struct{}{}
		}
		start := time.UnixMicro(rawSpan.StartTime).UTC()
		finish := start.Add(time.Duration(rawSpan.Duration) * time.Microsecond)
		if earliest.IsZero() || start.Before(earliest) {
			earliest = start
		}
		if latest.IsZero() || finish.After(latest) {
			latest = finish
		}
		parentID := parentSpanID(rawSpan.References, raw.TraceID)
		if parentID == "" && traceInfo.RootSpanID == "" {
			traceInfo.RootSpanID = rawSpan.SpanID
		}
		traceInfo.Spans = append(traceInfo.Spans, entities.TraceSpan{
			TraceID:        rawSpan.TraceID,
			SpanID:         rawSpan.SpanID,
			ParentSpanID:   parentID,
			OperationName:  rawSpan.OperationName,
			ServiceName:    process.ServiceName,
			StartTime:      start,
			DurationMicros: rawSpan.Duration,
			Status:         spanStatus(attributes),
			Kind:           stringAttribute(attributes, "span.kind"),
			Attributes:     attributes,
			Events:         normalizeEvents(rawSpan.Logs),
			Warnings:       rawSpan.Warnings,
		})
	}
	if !matches {
		return entities.TraceInfo{}, false
	}
	traceInfo.StartTime = earliest
	traceInfo.DurationMicros = latest.Sub(earliest).Microseconds()
	for service := range serviceSet {
		traceInfo.Services = append(traceInfo.Services, service)
	}
	sort.Strings(traceInfo.Services)
	sort.SliceStable(traceInfo.Spans, func(i, j int) bool {
		return traceInfo.Spans[i].StartTime.Before(traceInfo.Spans[j].StartTime)
	})
	return traceInfo, true
}

func tagsToMap(tags []jaegerTag) map[string]any {
	result := make(map[string]any, len(tags))
	for _, tag := range tags {
		if tag.Key != "" {
			result[tag.Key] = tag.Value
		}
	}
	return result
}

func parentSpanID(refs []jaegerReference, traceID string) string {
	for _, ref := range refs {
		if ref.TraceID == traceID && strings.EqualFold(ref.RefType, "CHILD_OF") {
			return ref.SpanID
		}
	}
	for _, ref := range refs {
		if ref.TraceID == traceID {
			return ref.SpanID
		}
	}
	return ""
}

func normalizeEvents(logs []jaegerLog) []entities.TraceEvent {
	if len(logs) == 0 {
		return nil
	}
	events := make([]entities.TraceEvent, 0, len(logs))
	for _, logEntry := range logs {
		attributes := tagsToMap(logEntry.Fields)
		name := stringAttribute(attributes, "event")
		if name == "" {
			name = stringAttribute(attributes, "name")
		}
		events = append(events, entities.TraceEvent{
			Timestamp:  time.UnixMicro(logEntry.Timestamp).UTC(),
			Name:       name,
			Attributes: attributes,
		})
	}
	return events
}

func spanStatus(attributes map[string]any) string {
	status := strings.ToUpper(stringAttribute(attributes, "otel.status_code"))
	if status == "ERROR" || isTruthy(attributes["error"]) {
		return "ERROR"
	}
	if status == "OK" {
		return "OK"
	}
	if code, ok := numberValue(attributes["http.response.status_code"]); ok && code >= 500 {
		return "ERROR"
	}
	return "UNSET"
}

func stringAttribute(attributes map[string]any, key string) string {
	value, ok := attributes[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func isTruthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	default:
		return false
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
