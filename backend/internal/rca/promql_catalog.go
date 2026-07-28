package rca

import (
	"fmt"
	"strings"
	"unicode"
)

const roundOnePromQLCatalogVersion = "v1"

type controlledPromQL struct {
	Key         string
	Description string
	Query       string
}

type promLabelScope struct {
	Service     string
	Environment string
	Namespace   string
}

func roundOnePromQL(scope promLabelScope) ([]controlledPromQL, error) {
	service, err := validatePromLabelValue(scope.Service)
	if err != nil {
		return nil, err
	}
	matchers := []string{`service="` + escapePromLabelValue(service) + `"`}
	for _, label := range []struct {
		key   string
		value string
	}{{key: "environment", value: scope.Environment}, {key: "namespace", value: scope.Namespace}} {
		key, value := label.key, label.value
		if strings.TrimSpace(value) == "" {
			continue
		}
		validated, err := validatePromLabelValue(value)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, key+`="`+escapePromLabelValue(validated)+`"`)
	}
	selector := strings.Join(matchers, ",")
	return []controlledPromQL{
		{
			Key:         "latency_p99",
			Description: "HTTP request P99 latency",
			Query:       fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(http_server_request_duration_seconds_bucket{%s}[5m])))`, selector),
		},
		{
			Key:         "error_rate",
			Description: "HTTP 5xx error ratio",
			Query: fmt.Sprintf(
				`sum(rate(http_server_requests_total{%s,status=~"5.."}[5m])) / clamp_min(sum(rate(http_server_requests_total{%s}[5m])), 1)`,
				selector, selector,
			),
		},
		{
			Key:         "qps",
			Description: "HTTP request throughput",
			Query:       fmt.Sprintf(`sum(rate(http_server_requests_total{%s}[5m]))`, selector),
		},
		{
			Key:         "resource_saturation",
			Description: "Process CPU saturation",
			Query:       fmt.Sprintf(`max(rate(process_cpu_seconds_total{%s}[5m]))`, selector),
		},
	}, nil
}

func validatePromLabelValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 128 {
		return "", ErrInvalidInput
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			continue
		}
		switch character {
		case '-', '_', '.', ':', '/':
			continue
		default:
			return "", ErrInvalidInput
		}
	}
	return value, nil
}

func escapePromLabelValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}
