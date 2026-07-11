package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestQueryRangeEnforcesSeriesAndPointLimits(t *testing.T) {
	client := fakeHTTPClient(t, func(r *http.Request) any {
		if r.URL.Path != "/api/v1/query_range" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Fatalf("prometheus limit param = %q, want 2", got)
		}
		return prometheusMatrixResponse(4, 5)
	})
	service := NewService(testRepository{dataSource: testDataSource(t, "https://prometheus.example.test")}, nil, client)

	result, err := service.Query(context.Background(), testActor(), QueryInput{
		DataSourceID: 1,
		Query:        `rate(http_requests_total[5m])`,
		Range:        true,
		Start:        time.Unix(1000, 0),
		End:          time.Unix(1300, 0),
		Step:         time.Minute,
		MaxSeries:    2,
		MaxPoints:    3,
	})
	if err != nil {
		t.Fatalf("query range: %v", err)
	}
	if len(result.Series) != 2 {
		t.Fatalf("series count = %d, want 2", len(result.Series))
	}
	for _, series := range result.Series {
		if len(series.Points) != 3 {
			t.Fatalf("point count = %d, want 3", len(series.Points))
		}
		if series.Points[0].RawValue != "2" {
			t.Fatalf("expected oldest kept point to be 2, got %+v", series.Points)
		}
	}
	if result.Limit.MaxSeries != 2 || result.Limit.MaxPoints != 3 {
		t.Fatalf("unexpected limit echo: %+v", result.Limit)
	}
}

func TestQueryInstantVector(t *testing.T) {
	client := fakeHTTPClient(t, func(r *http.Request) any {
		if r.URL.Path != "/api/v1/query" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{
					map[string]any{
						"metric": map[string]string{"__name__": "up", "job": "api"},
						"value":  []any{1700000000.0, "1"},
					},
				},
			},
		}
	})
	service := NewService(testRepository{dataSource: testDataSource(t, "https://prometheus.example.test")}, nil, client)

	result, err := service.Query(context.Background(), testActor(), QueryInput{DataSourceID: 1, Query: "up", MaxSeries: 5, MaxPoints: 5})
	if err != nil {
		t.Fatalf("query instant: %v", err)
	}
	if len(result.Series) != 1 || result.Series[0].Metric["job"] != "api" || result.Series[0].Points[0].Value != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestTestUsesReadableQueryAPI(t *testing.T) {
	client := fakeHTTPClient(t, func(r *http.Request) any {
		if r.URL.Query().Get("query") != "up" {
			t.Fatalf("expected up query, got %q", r.URL.Query().Get("query"))
		}
		return map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result": []any{
					map[string]any{"metric": map[string]string{"job": "prometheus"}, "value": []any{1700000000.0, "1"}},
				},
			},
		}
	})
	service := NewService(testRepository{dataSource: testDataSource(t, "https://prometheus.example.test")}, nil, client)

	result, err := service.Test(context.Background(), testActor(), 1)
	if err != nil {
		t.Fatalf("test prometheus: %v", err)
	}
	if !result.OK {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func fakeHTTPClient(t *testing.T, handler func(*http.Request) any) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := json.Marshal(handler(request))
		if err != nil {
			t.Fatalf("marshal fake response: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    request,
		}, nil
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func prometheusMatrixResponse(seriesCount int, pointCount int) map[string]any {
	result := make([]any, 0, seriesCount)
	for seriesIndex := 0; seriesIndex < seriesCount; seriesIndex++ {
		points := make([]any, 0, pointCount)
		for pointIndex := 0; pointIndex < pointCount; pointIndex++ {
			points = append(points, []any{float64(1700000000 + pointIndex), strconv.Itoa(pointIndex)})
		}
		result = append(result, map[string]any{
			"metric": map[string]string{"__name__": "http_requests_total", "instance": strconv.Itoa(seriesIndex)},
			"values": points,
		})
	}
	return map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "matrix", "result": result},
	}
}

type testRepository struct {
	dataSource *model.DataSource
}

func (r testRepository) FindDataSourceByID(_ context.Context, id int64) (*model.DataSource, error) {
	if r.dataSource != nil && r.dataSource.ID == id {
		return r.dataSource, nil
	}
	return nil, errors.New("not found")
}

func testDataSource(t *testing.T, baseURL string) *model.DataSource {
	t.Helper()
	raw, err := json.Marshal(promConfig{BaseURL: baseURL})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return &model.DataSource{
		ID:         1,
		Name:       "prometheus",
		SourceType: model.DataSourceTypePrometheus,
		Config:     raw,
		Enabled:    true,
		ReadOnly:   true,
	}
}

func testActor() *model.AppUser {
	return &model.AppUser{ID: 10, Username: "operator", Role: model.RoleUser, Enabled: true}
}
