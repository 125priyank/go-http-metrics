package prometheus

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/slok/go-http-metrics/metrics"
)

type CustomLabels interface {
	Reporter(handlerID string, method string, body []byte) map[string]string
	GetLabels() []string
}

type Labels struct {
	// HandlerIDLabel is the name that will be set to the handler ID label, by default is `handler`.
	HandlerIDLabel string
	// StatusCodeLabel is the name that will be set to the status code label, by default is `code`.
	StatusCodeLabel string
	// MethodLabel is the name that will be set to the method label, by default is `method`.
	MethodLabel string
	// ServiceLabel is the name that will be set to the service label, by default is `service`.
	ServiceLabel string
	// CustomLabels can be used to initialize custom labels in http metrics.
	CustomLabels CustomLabels
}

// Config has the dependencies and values of the recorder.
type Config struct {
	Labels
	// Prefix is the prefix that will be set on the metrics, by default it will be empty.
	Prefix string
	// DurationBuckets are the buckets used by Prometheus for the HTTP request duration metrics,
	// by default uses Prometheus default buckets (from 5ms to 10s).
	DurationBuckets []float64
	// SizeBuckets are the buckets used by Prometheus for the HTTP response size metrics,
	// by default uses a exponential buckets from 100B to 1GB.
	SizeBuckets []float64
	// Registry is the registry that will be used by the recorder to store the metrics,
	// if the default registry is not used then it will use the default one.
	Registry prometheus.Registerer
}

func (c *Config) defaults() {
	if len(c.DurationBuckets) == 0 {
		c.DurationBuckets = prometheus.DefBuckets
	}

	if len(c.SizeBuckets) == 0 {
		c.SizeBuckets = prometheus.ExponentialBuckets(100, 10, 8)
	}

	if c.Registry == nil {
		c.Registry = prometheus.DefaultRegisterer
	}

	if c.HandlerIDLabel == "" {
		c.HandlerIDLabel = "handler"
	}

	if c.StatusCodeLabel == "" {
		c.StatusCodeLabel = "code"
	}

	if c.MethodLabel == "" {
		c.MethodLabel = "method"
	}

	if c.ServiceLabel == "" {
		c.ServiceLabel = "service"
	}
}

type recorder struct {
	httpRequestDurHistogram   *prometheus.HistogramVec
	httpResponseSizeHistogram *prometheus.HistogramVec
	httpRequestsInflight      *prometheus.GaugeVec
	labels                    *Labels
}

// NewRecorder returns a new metrics recorder that implements the recorder
// using Prometheus as the backend.
func NewRecorder(cfg Config) metrics.Recorder {
	cfg.defaults()

	var customLabels []string
	if cfg.CustomLabels != nil {
		customLabels = cfg.CustomLabels.GetLabels()
	}

	r := &recorder{
		httpRequestDurHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "The latency of the HTTP requests.",
			Buckets:   cfg.DurationBuckets,
		}, append([]string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel}, customLabels...)),

		httpResponseSizeHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "The size of the HTTP responses.",
			Buckets:   cfg.SizeBuckets,
		}, append([]string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel}, customLabels...)),

		httpRequestsInflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "requests_inflight",
			Help:      "The number of inflight requests being handled at the same time.",
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel}),
		labels: &cfg.Labels,
	}

	cfg.Registry.MustRegister(
		r.httpRequestDurHistogram,
		r.httpResponseSizeHistogram,
		r.httpRequestsInflight,
	)

	return r
}

func (r recorder) ObserveHTTPRequestDuration(_ context.Context, p metrics.HTTPReqProperties, duration time.Duration) {
	// If custom labels are not defined then it is better to record metrics using WithLabelValues as reporting
	// with With() has performance overhead due to using maps.
	if r.labels.CustomLabels == nil {
		r.httpRequestDurHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(duration.Seconds())
		return
	}

	labels := prometheus.Labels{r.labels.ServiceLabel: p.Service, r.labels.HandlerIDLabel: p.ID,
		r.labels.MethodLabel: p.Method, r.labels.StatusCodeLabel: p.Code}
	customMetrics := r.labels.CustomLabels.Reporter(p.ID, p.Method, p.Body)
	for _, label := range r.labels.CustomLabels.GetLabels() {
		labels[label] = customMetrics[label]
	}
	r.httpRequestDurHistogram.With(labels).Observe(duration.Seconds())
}

func (r recorder) ObserveHTTPResponseSize(_ context.Context, p metrics.HTTPReqProperties, sizeBytes int64) {
	// If custom labels are not defined then it is better to record metrics using WithLabelValues as reporting
	// with With() has performance overhead due to using maps.
	if r.labels.CustomLabels == nil {
		r.httpResponseSizeHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(float64(sizeBytes))
		return
	}

	labels := prometheus.Labels{r.labels.ServiceLabel: p.Service, r.labels.HandlerIDLabel: p.ID,
		r.labels.MethodLabel: p.Method, r.labels.StatusCodeLabel: p.Code}
	customMetrics := r.labels.CustomLabels.Reporter(p.ID, p.Method, p.Body)
	for _, label := range r.labels.CustomLabels.GetLabels() {
		labels[label] = customMetrics[label]
	}
	r.httpResponseSizeHistogram.With(labels).Observe(float64(sizeBytes))
}

func (r recorder) AddInflightRequests(_ context.Context, p metrics.HTTPProperties, quantity int) {
	r.httpRequestsInflight.WithLabelValues(p.Service, p.ID).Add(float64(quantity))
}
