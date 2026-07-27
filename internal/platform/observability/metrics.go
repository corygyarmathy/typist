// Package observability provides Prometheus metrics and (later) OpenTelemetry
// tracing setup.
//
// Metrics exposed via the /metrics endpoint:
//   - http_requests_total{method, route, status}
//   - http_request_duration_seconds{method, route}
//   - db_query_duration_seconds{query}
//   - engine_lesson_generated_total
//
// Metrics handlers are wired in cmd/server/main.go and exposed on the
// same HTTP server as the API (separate listener is overkill at this size).
package observability

// TODO(phase-6): expose RegisterMetrics(reg *prometheus.Registry) returning
// the metric handles, and a Handler() http.Handler for /metrics.
