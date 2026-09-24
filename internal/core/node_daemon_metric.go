package core

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	defaultNodeDaemonMetricLimit        = 100
	maxNodeDaemonMetricLimit            = 200
	maxNodeDaemonMetricFilters          = 32
	maxNodeDaemonMetricFilterRunes      = 255
	maxNodeDaemonMetricCursorRunes      = 1024
	maxNodeDaemonMetricPayloadBytes     = 256 << 10
	maxNodeDaemonMetricNameRunes        = 1024
	maxNodeDaemonMetricHelpRunes        = 4096
	maxNodeDaemonMetricSamples          = 5000
	maxNodeDaemonMetricSamplesPerFamily = 1000
	maxNodeDaemonMetricLabelsPerSample  = 64
	maxNodeDaemonMetricLabelRunes       = 4096
	maxNodeDaemonMetricPointsPerSample  = 1000
)

type GetNodeDaemonMetricsOptions struct {
	Node     string
	Names    []string
	Prefixes []string
	Limit    int
	Cursor   string
}

type NodeDaemonMetricList struct {
	Provenance          Provenance               `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	TargetNode          string                   `json:"target_node" jsonschema:"exact requested OpenSVC node name or the underscore alias for the local daemon node"`
	ReportedTotal       int                      `json:"reported_total" jsonschema:"number of metric families parsed from the daemon response before filtering"`
	Total               int                      `json:"total" jsonschema:"number of metric families matching the requested exact names or prefixes before pagination"`
	SampleTotal         int                      `json:"sample_total" jsonschema:"number of samples across all matching metric families before pagination"`
	Count               int                      `json:"count" jsonschema:"number of metric families returned in this page"`
	ReturnedSampleCount int                      `json:"returned_sample_count" jsonschema:"number of samples across the metric families returned in this page"`
	Metrics             []NodeDaemonMetricFamily `json:"metrics" jsonschema:"matching Prometheus metric families sorted by exact family name"`
	NextCursor          string                   `json:"next_cursor,omitempty" jsonschema:"exact metric family name to pass unchanged for the next page"`
	Truncated           bool                     `json:"truncated" jsonschema:"whether matching metric families remain after this page"`
}

type NodeDaemonMetricFamily struct {
	Name        string                   `json:"name" jsonschema:"exact Prometheus metric family name"`
	Help        string                   `json:"help,omitempty" jsonschema:"HELP text reported for this Prometheus metric family"`
	Type        string                   `json:"type" jsonschema:"Prometheus metric family type in lowercase"`
	SampleCount int                      `json:"sample_count" jsonschema:"number of samples returned for this metric family"`
	Samples     []NodeDaemonMetricSample `json:"samples" jsonschema:"typed samples reported for this metric family"`
}

type NodeDaemonMetricSample struct {
	Labels      []NodeDaemonMetricLabel    `json:"labels" jsonschema:"Prometheus labels sorted by exact label name"`
	TimestampMS *int64                     `json:"timestamp_ms,omitempty" jsonschema:"optional sample timestamp in Unix milliseconds when supplied by the endpoint"`
	Value       string                     `json:"value,omitempty" jsonschema:"counter gauge or untyped value encoded as a decimal string or NaN plus Inf or minus Inf"`
	Summary     *NodeDaemonMetricSummary   `json:"summary,omitempty" jsonschema:"summary observation data when the family type is summary"`
	Histogram   *NodeDaemonMetricHistogram `json:"histogram,omitempty" jsonschema:"histogram observation data when the family type is histogram"`
}

type NodeDaemonMetricLabel struct {
	Name  string `json:"name" jsonschema:"exact Prometheus label name"`
	Value string `json:"value" jsonschema:"exact Prometheus label value"`
}

type NodeDaemonMetricSummary struct {
	SampleCount uint64                            `json:"sample_count" jsonschema:"cumulative number of observations in the summary"`
	SampleSum   string                            `json:"sample_sum" jsonschema:"cumulative observation sum encoded as a decimal string or special Prometheus value"`
	Quantiles   []NodeDaemonMetricSummaryQuantile `json:"quantiles" jsonschema:"reported summary quantiles sorted by quantile"`
}

type NodeDaemonMetricSummaryQuantile struct {
	Quantile string `json:"quantile" jsonschema:"quantile rank encoded as a decimal string"`
	Value    string `json:"value" jsonschema:"reported quantile value encoded as a decimal string or special Prometheus value"`
}

type NodeDaemonMetricHistogram struct {
	SampleCount uint64                            `json:"sample_count" jsonschema:"cumulative number of observations in the histogram"`
	SampleSum   string                            `json:"sample_sum" jsonschema:"cumulative observation sum encoded as a decimal string or special Prometheus value"`
	Buckets     []NodeDaemonMetricHistogramBucket `json:"buckets" jsonschema:"histogram buckets sorted by upper bound"`
}

type NodeDaemonMetricHistogramBucket struct {
	UpperBound      string `json:"upper_bound" jsonschema:"inclusive bucket upper bound encoded as a decimal string or plus Inf"`
	CumulativeCount uint64 `json:"cumulative_count" jsonschema:"cumulative observations at or below this upper bound"`
}

func (s *Service) GetNodeDaemonMetrics(ctx context.Context, options GetNodeDaemonMetricsOptions) (NodeDaemonMetricList, error) {
	node, names, prefixes, limit, cursor, err := validateNodeDaemonMetricOptions(options)
	if err != nil {
		return NodeDaemonMetricList{}, err
	}
	client, ok := s.client.(TextGetter)
	if !ok {
		return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: daemon client does not support text responses")
	}
	endpoint := fmt.Sprintf("/api/node/name/%s/metrics", node)
	payload, err := client.GetText(ctx, endpoint, url.Values{})
	if err != nil {
		return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: %w", err)
	}
	if len(payload) > maxNodeDaemonMetricPayloadBytes {
		return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: response exceeds the %d-byte diagnostic limit; use a narrower daemon endpoint", maxNodeDaemonMetricPayloadBytes)
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)
	parsed, err := parser.TextToMetricFamilies(bytes.NewReader(payload))
	if err != nil {
		return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: parse Prometheus text response: %w", err)
	}
	familyNames := make([]string, 0, len(parsed))
	for name := range parsed {
		if matchesNodeDaemonMetricFilter(name, names, prefixes) {
			familyNames = append(familyNames, name)
		}
	}
	sort.Strings(familyNames)
	start := 0
	if cursor != "" {
		index := sort.SearchStrings(familyNames, cursor)
		if index == len(familyNames) || familyNames[index] != cursor {
			return NodeDaemonMetricList{}, fmt.Errorf("node daemon metric cursor is no longer present in the filtered result")
		}
		start = index + 1
	}

	sampleTotal := 0
	for _, name := range familyNames {
		sampleTotal += len(parsed[name].Metric)
	}
	if sampleTotal > maxNodeDaemonMetricSamples {
		return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: filtered response contains %d samples, limit is %d; use names or prefixes to narrow it", sampleTotal, maxNodeDaemonMetricSamples)
	}
	end := min(start+limit, len(familyNames))
	metrics := make([]NodeDaemonMetricFamily, 0, end-start)
	returnedSampleCount := 0
	for _, name := range familyNames[start:end] {
		family, err := projectNodeDaemonMetricFamily(name, parsed[name])
		if err != nil {
			return NodeDaemonMetricList{}, fmt.Errorf("get node daemon metrics: %w", err)
		}
		returnedSampleCount += family.SampleCount
		metrics = append(metrics, family)
	}
	result := NodeDaemonMetricList{
		Provenance:          s.newProvenance(),
		TargetNode:          node,
		ReportedTotal:       len(parsed),
		Total:               len(familyNames),
		SampleTotal:         sampleTotal,
		Count:               len(metrics),
		ReturnedSampleCount: returnedSampleCount,
		Metrics:             metrics,
		Truncated:           end < len(familyNames),
	}
	if result.Truncated {
		result.NextCursor = familyNames[end-1]
	}
	return result, nil
}

func validateNodeDaemonMetricOptions(options GetNodeDaemonMetricsOptions) (string, []string, []string, int, string, error) {
	node := options.Node
	if node == "" {
		node = localDaemonNodeAlias
	} else if node != localDaemonNodeAlias && !validExactNodeName(node) {
		return "", nil, nil, 0, "", fmt.Errorf("node must be one exact OpenSVC node name of at most 255 characters")
	}
	names, err := normalizeNodeDaemonMetricFilters("name", options.Names)
	if err != nil {
		return "", nil, nil, 0, "", err
	}
	prefixes, err := normalizeNodeDaemonMetricFilters("prefix", options.Prefixes)
	if err != nil {
		return "", nil, nil, 0, "", err
	}
	limit := options.Limit
	if limit == 0 {
		limit = defaultNodeDaemonMetricLimit
	}
	if limit < 1 || limit > maxNodeDaemonMetricLimit {
		return "", nil, nil, 0, "", fmt.Errorf("node daemon metric limit must be between 1 and %d", maxNodeDaemonMetricLimit)
	}
	if len([]rune(options.Cursor)) > maxNodeDaemonMetricCursorRunes || strings.ContainsAny(options.Cursor, "\r\n\x00") {
		return "", nil, nil, 0, "", fmt.Errorf("node daemon metric cursor exceeds %d characters or contains control characters", maxNodeDaemonMetricCursorRunes)
	}
	return node, names, prefixes, limit, options.Cursor, nil
}

func normalizeNodeDaemonMetricFilters(kind string, values []string) ([]string, error) {
	if len(values) > maxNodeDaemonMetricFilters {
		return nil, fmt.Errorf("node daemon metric %ss are limited to %d entries", kind, maxNodeDaemonMetricFilters)
	}
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" || len([]rune(value)) > maxNodeDaemonMetricFilterRunes || strings.ContainsAny(value, "\r\n\x00\t ") {
			return nil, fmt.Errorf("node daemon metric %s must be non-empty, at most %d characters, and contain no whitespace or control characters", kind, maxNodeDaemonMetricFilterRunes)
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func matchesNodeDaemonMetricFilter(name string, names, prefixes []string) bool {
	if len(names) == 0 && len(prefixes) == 0 {
		return true
	}
	if index := sort.SearchStrings(names, name); index < len(names) && names[index] == name {
		return true
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func projectNodeDaemonMetricFamily(name string, family *dto.MetricFamily) (NodeDaemonMetricFamily, error) {
	if family == nil || name == "" || family.GetName() != name || len([]rune(name)) > maxNodeDaemonMetricNameRunes || strings.ContainsAny(name, "\r\n\x00") {
		return NodeDaemonMetricFamily{}, fmt.Errorf("metric family %q has an invalid name", name)
	}
	if len([]rune(family.GetHelp())) > maxNodeDaemonMetricHelpRunes || strings.ContainsAny(family.GetHelp(), "\r\x00") {
		return NodeDaemonMetricFamily{}, fmt.Errorf("metric family %q has oversized or control-containing HELP text", name)
	}
	if len(family.Metric) > maxNodeDaemonMetricSamplesPerFamily {
		return NodeDaemonMetricFamily{}, fmt.Errorf("metric family %q contains %d samples, limit is %d", name, len(family.Metric), maxNodeDaemonMetricSamplesPerFamily)
	}
	typeName := strings.ToLower(family.GetType().String())
	if family.Type == nil || typeName == "" {
		return NodeDaemonMetricFamily{}, fmt.Errorf("metric family %q has no type", name)
	}
	samples := make([]NodeDaemonMetricSample, 0, len(family.Metric))
	for index, metric := range family.Metric {
		sample, err := projectNodeDaemonMetricSample(family.GetType(), metric)
		if err != nil {
			return NodeDaemonMetricFamily{}, fmt.Errorf("metric family %q sample %d: %w", name, index, err)
		}
		samples = append(samples, sample)
	}
	sort.SliceStable(samples, func(i, j int) bool {
		return nodeDaemonMetricSampleKey(samples[i]) < nodeDaemonMetricSampleKey(samples[j])
	})
	return NodeDaemonMetricFamily{Name: name, Help: family.GetHelp(), Type: typeName, SampleCount: len(samples), Samples: samples}, nil
}

func projectNodeDaemonMetricSample(metricType dto.MetricType, metric *dto.Metric) (NodeDaemonMetricSample, error) {
	if metric == nil {
		return NodeDaemonMetricSample{}, fmt.Errorf("sample is null")
	}
	if len(metric.Label) > maxNodeDaemonMetricLabelsPerSample {
		return NodeDaemonMetricSample{}, fmt.Errorf("sample contains %d labels, limit is %d", len(metric.Label), maxNodeDaemonMetricLabelsPerSample)
	}
	labels := make([]NodeDaemonMetricLabel, 0, len(metric.Label))
	for _, pair := range metric.Label {
		if pair == nil || pair.Name == nil || pair.Value == nil || pair.GetName() == "" ||
			len([]rune(pair.GetName())) > maxNodeDaemonMetricNameRunes || len([]rune(pair.GetValue())) > maxNodeDaemonMetricLabelRunes ||
			strings.ContainsAny(pair.GetName(), "\r\n\x00") || strings.ContainsAny(pair.GetValue(), "\r\n\x00") {
			return NodeDaemonMetricSample{}, fmt.Errorf("sample contains an invalid label")
		}
		labels = append(labels, NodeDaemonMetricLabel{Name: pair.GetName(), Value: pair.GetValue()})
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i].Name < labels[j].Name })
	result := NodeDaemonMetricSample{Labels: labels, TimestampMS: metric.TimestampMs}
	switch metricType {
	case dto.MetricType_COUNTER:
		if metric.Counter == nil {
			return NodeDaemonMetricSample{}, fmt.Errorf("counter value is absent")
		}
		result.Value = formatNodeDaemonMetricNumber(metric.Counter.GetValue())
	case dto.MetricType_GAUGE:
		if metric.Gauge == nil {
			return NodeDaemonMetricSample{}, fmt.Errorf("gauge value is absent")
		}
		result.Value = formatNodeDaemonMetricNumber(metric.Gauge.GetValue())
	case dto.MetricType_UNTYPED:
		if metric.Untyped == nil {
			return NodeDaemonMetricSample{}, fmt.Errorf("untyped value is absent")
		}
		result.Value = formatNodeDaemonMetricNumber(metric.Untyped.GetValue())
	case dto.MetricType_SUMMARY:
		if metric.Summary == nil || len(metric.Summary.Quantile) > maxNodeDaemonMetricPointsPerSample {
			return NodeDaemonMetricSample{}, fmt.Errorf("summary is absent or contains more than %d quantiles", maxNodeDaemonMetricPointsPerSample)
		}
		quantiles := make([]NodeDaemonMetricSummaryQuantile, 0, len(metric.Summary.Quantile))
		for _, quantile := range metric.Summary.Quantile {
			if quantile == nil || quantile.Quantile == nil || quantile.Value == nil {
				return NodeDaemonMetricSample{}, fmt.Errorf("summary contains an incomplete quantile")
			}
			quantiles = append(quantiles, NodeDaemonMetricSummaryQuantile{Quantile: formatNodeDaemonMetricNumber(quantile.GetQuantile()), Value: formatNodeDaemonMetricNumber(quantile.GetValue())})
		}
		sort.Slice(quantiles, func(i, j int) bool {
			left, _ := strconv.ParseFloat(quantiles[i].Quantile, 64)
			right, _ := strconv.ParseFloat(quantiles[j].Quantile, 64)
			return left < right
		})
		result.Summary = &NodeDaemonMetricSummary{SampleCount: metric.Summary.GetSampleCount(), SampleSum: formatNodeDaemonMetricNumber(metric.Summary.GetSampleSum()), Quantiles: quantiles}
	case dto.MetricType_HISTOGRAM:
		if metric.Histogram == nil || len(metric.Histogram.Bucket) > maxNodeDaemonMetricPointsPerSample {
			return NodeDaemonMetricSample{}, fmt.Errorf("histogram is absent or contains more than %d buckets", maxNodeDaemonMetricPointsPerSample)
		}
		buckets := make([]NodeDaemonMetricHistogramBucket, 0, len(metric.Histogram.Bucket))
		for _, bucket := range metric.Histogram.Bucket {
			if bucket == nil || bucket.UpperBound == nil || bucket.CumulativeCount == nil {
				return NodeDaemonMetricSample{}, fmt.Errorf("histogram contains an incomplete bucket")
			}
			buckets = append(buckets, NodeDaemonMetricHistogramBucket{UpperBound: formatNodeDaemonMetricNumber(bucket.GetUpperBound()), CumulativeCount: bucket.GetCumulativeCount()})
		}
		sort.Slice(buckets, func(i, j int) bool {
			left, _ := strconv.ParseFloat(buckets[i].UpperBound, 64)
			right, _ := strconv.ParseFloat(buckets[j].UpperBound, 64)
			return left < right
		})
		result.Histogram = &NodeDaemonMetricHistogram{SampleCount: metric.Histogram.GetSampleCount(), SampleSum: formatNodeDaemonMetricNumber(metric.Histogram.GetSampleSum()), Buckets: buckets}
	default:
		return NodeDaemonMetricSample{}, fmt.Errorf("unsupported Prometheus metric type %q", metricType.String())
	}
	return result, nil
}

func formatNodeDaemonMetricNumber(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func nodeDaemonMetricSampleKey(sample NodeDaemonMetricSample) string {
	var key strings.Builder
	for _, label := range sample.Labels {
		key.WriteString(label.Name)
		key.WriteByte(0)
		key.WriteString(label.Value)
		key.WriteByte(0)
	}
	if sample.TimestampMS != nil {
		key.WriteString(strconv.FormatInt(*sample.TimestampMS, 10))
	}
	return key.String()
}
