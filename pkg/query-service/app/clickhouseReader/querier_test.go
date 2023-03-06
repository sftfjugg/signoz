package clickhouseReader

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.signoz.io/signoz/pkg/query-service/cache/inmemory"
	v3 "go.signoz.io/signoz/pkg/query-service/model/v3"
)

func TestFindMissingTimeRangesZeroFreshNess(t *testing.T) {
	// There are five scenarios:
	// 1. Cached time range is a subset of the requested time range
	// 2. Cached time range is a superset of the requested time range
	// 3. Cached time range is a left overlap of the requested time range
	// 4. Cached time range is a right overlap of the requested time range
	// 5. Cached time range is a disjoint of the requested time range
	type Range struct {
		start int64 // in milliseconds
		end   int64 // in milliseconds
	}
	testCases := []struct {
		name           string
		requestedStart int64 // in milliseconds
		requestedEnd   int64 // in milliseconds
		cachedSeries   []*v3.Series
		expectedMiss   []miss
	}{
		{
			name:           "cached time range is a subset of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
					},
				},
			},
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 60*60*1000 - 1,
				},
				{
					start: 1675115596722 + 120*60*1000 + 1,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
		{
			name:           "cached time range is a superset of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 180*60*1000,
							Value:     1,
						},
					},
				},
			},
			expectedMiss: []miss{},
		},
		{
			name:           "cached time range is a left overlap of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
					},
				},
			},
			expectedMiss: []miss{
				{
					start: 1675115596722 + 120*60*1000 + 1,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
		{
			name:           "cached time range is a right overlap of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 180*60*1000,
							Value:     1,
						},
					},
				},
			},
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 60*60*1000 - 1,
				},
			},
		},
		{
			name:           "cached time range is a disjoint of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 240*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 300*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 360*60*1000,
							Value:     1,
						},
					},
				},
			},
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			misses := findMissingTimeRanges(tc.requestedStart, tc.requestedEnd, tc.cachedSeries, 0*time.Minute)
			if len(misses) != len(tc.expectedMiss) {
				fmt.Println(misses, tc.expectedMiss)
				t.Errorf("expected %d misses, got %d", len(tc.expectedMiss), len(misses))
			}
			for i, miss := range misses {
				if miss.start != tc.expectedMiss[i].start {
					t.Errorf("expected start %d, got %d", tc.expectedMiss[i].start, miss.start)
				}
				if miss.end != tc.expectedMiss[i].end {
					t.Errorf("expected end %d, got %d", tc.expectedMiss[i].end, miss.end)
				}
			}
		})
	}
}

func TestFindMissingTimeRangesWithFluxInterval(t *testing.T) {
	type miss struct {
		start int64
		end   int64
	}

	testCases := []struct {
		name           string
		requestedStart int64
		requestedEnd   int64
		cachedSeries   []*v3.Series
		fluxInterval   time.Duration
		expectedMiss   []miss
	}{
		{
			name:           "cached time range is a subset of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
					},
				},
			},
			fluxInterval: 5 * time.Minute,
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 60*60*1000 - 1,
				},
				{
					start: 1675115596722 + 120*60*1000 + 1,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
		{
			name:           "cached time range is a superset of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 180*60*1000,
							Value:     1,
						},
					},
				},
			},
			fluxInterval: 5 * time.Minute,
			expectedMiss: []miss{},
		},
		{
			name:           "cache time range is a left overlap of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
					},
				},
			},
			fluxInterval: 5 * time.Minute,
			expectedMiss: []miss{
				{
					start: 1675115596722 + 120*60*1000 + 1,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
		{
			name:           "cache time range is a right overlap of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 60*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 120*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 180*60*1000,
							Value:     1,
						},
					},
				},
			},
			fluxInterval: 5 * time.Minute,
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 60*60*1000 - 1,
				},
			},
		},
		{
			name:           "cache time range is a disjoint of the requested time range",
			requestedStart: 1675115596722,
			requestedEnd:   1675115596722 + 180*60*1000,
			cachedSeries: []*v3.Series{
				{
					Labels: map[string]string{
						"__name__": "http_server_requests_seconds_count",
					},
					Points: []v3.Point{
						{
							Timestamp: 1675115596722 + 240*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 300*60*1000,
							Value:     1,
						},
						{
							Timestamp: 1675115596722 + 360*60*1000,
							Value:     1,
						},
					},
				},
			},
			fluxInterval: 5 * time.Minute,
			expectedMiss: []miss{
				{
					start: 1675115596722,
					end:   1675115596722 + 180*60*1000,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			misses := findMissingTimeRanges(tc.requestedStart, tc.requestedEnd, tc.cachedSeries, tc.fluxInterval)
			if len(misses) != len(tc.expectedMiss) {
				fmt.Println(misses, tc.expectedMiss)
				t.Errorf("expected %d misses, got %d", len(tc.expectedMiss), len(misses))
			}
			for i, miss := range misses {
				if miss.start != tc.expectedMiss[i].start {
					t.Errorf("expected start %d, got %d", tc.expectedMiss[i].start, miss.start)
				}
				if miss.end != tc.expectedMiss[i].end {
					t.Errorf("expected end %d, got %d", tc.expectedMiss[i].end, miss.end)
				}
			}
		})
	}
}

func TestQueryRange(t *testing.T) {
	params := []*v3.QueryRangeParamsV3{
		{
			Start: 1675115596722,
			End:   1675115596722 + 120*60*1000,
			Step:  5 * time.Minute.Microseconds(),
			CompositeQuery: &v3.CompositeQuery{
				QueryType: v3.QueryTypeBuilder,
				BuilderQueries: map[string]*v3.BuilderQuery{
					"A": {
						QueryName:          "A",
						AggregateAttribute: "http_server_requests_seconds_count",
						Filters: &v3.FilterSet{
							Operator: "AND",
							Items: []v3.FilterItem{
								{
									Key:      "method",
									Operator: "EQ",
									Value:    "GET",
								},
							},
						},
						GroupBy:           []string{"service_name", "method"},
						AggregateOperator: v3.AggregateOperatorSumRate,
					},
				},
			},
		},
		{
			Start: 1675115596722 + 60*60*1000,
			End:   1675115596722 + 180*60*1000,
			Step:  5 * time.Minute.Microseconds(),
			CompositeQuery: &v3.CompositeQuery{
				QueryType: v3.QueryTypeBuilder,
				BuilderQueries: map[string]*v3.BuilderQuery{
					"A": {
						QueryName:          "A",
						AggregateAttribute: "http_server_requests_seconds_count",
						Filters: &v3.FilterSet{
							Operator: "AND",
							Items: []v3.FilterItem{
								{
									Key:      "method",
									Operator: "EQ",
									Value:    "GET",
								},
							},
						},
						GroupBy:           []string{"service_name", "method"},
						AggregateOperator: v3.AggregateOperatorSumRate,
					},
				},
			},
		},
	}
	cache := inmemory.New(&inmemory.Options{TTL: 5 * time.Minute, CleanupInterval: 10 * time.Minute})
	q := NewQuerier(cache, nil, 5*time.Minute)
	q.testingMode = true
	expectedTimeRangeInQueryString := []string{
		fmt.Sprintf("timestamp_ms >= %d AND timestamp_ms <= %d", 1675115596722, 1675115596722+120*60*1000),
		fmt.Sprintf("timestamp_ms >= %d AND timestamp_ms <= %d", 1675115596722+120*60*1000+1, 1675115596722+180*60*1000),
	}

	q.returnedSeries = []*v3.Series{
		{
			Labels: map[string]string{
				"method":       "GET",
				"service_name": "test",
				"__name__":     "http_server_requests_seconds_count",
			},
			Points: []v3.Point{
				{Timestamp: 1675115596722, Value: 1},
				{Timestamp: 1675115596722 + 60*60*1000, Value: 2},
				{Timestamp: 1675115596722 + 120*60*1000, Value: 3},
			},
		},
	}

	for i, param := range params {
		_, err, byName := q.QueryRange(context.Background(), param)
		if err != nil {
			t.Errorf("expected no error, got %s %v", err, byName)
		}
		if !strings.Contains(q.queriesExecuted[i], expectedTimeRangeInQueryString[i]) {
			t.Errorf("expected query to contain %s, got %s", expectedTimeRangeInQueryString[i], q.queriesExecuted[i])
		}
	}
}
