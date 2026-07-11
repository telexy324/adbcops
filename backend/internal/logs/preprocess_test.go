package logs

import (
	"strings"
	"testing"
	"time"

	"aiops-platform/backend/internal/model"
)

func TestPreprocessRedactsSensitiveValues(t *testing.T) {
	result := Preprocess(PreprocessInput{Items: []model.LogItem{{
		Timestamp: time.Date(2026, 7, 11, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		Level:     "err",
		Message:   "user phone=13812345678 id=110101199003071234 card=6222 0000 0000 0000 token=abc123 password=secret",
		Raw:       `{"mobile":"13812345678","password":"secret"}`,
	}}})
	if len(result.Items) != 1 {
		t.Fatalf("items = %d", len(result.Items))
	}
	item := result.Items[0]
	if item.Level != "ERROR" || item.Timestamp.Location() != time.UTC {
		t.Fatalf("normalized item = %+v", item)
	}
	combined := item.Message + "\n" + item.Raw
	for _, leaked := range []string{"13812345678", "110101199003071234", "6222 0000 0000 0000", "abc123", "secret"} {
		if strings.Contains(combined, leaked) {
			t.Fatalf("sensitive value %q leaked in %q", leaked, combined)
		}
	}
	for _, marker := range []string{"[REDACTED_PHONE]", "[REDACTED_ID_CARD]", "[REDACTED_CARD]", "[REDACTED_TOKEN]", "[REDACTED_PASSWORD]"} {
		if !strings.Contains(combined, marker) {
			t.Fatalf("redaction marker %q missing from %q", marker, combined)
		}
	}
	if result.RedactionCount < 5 {
		t.Fatalf("redaction count = %d, want >= 5", result.RedactionCount)
	}
}

func TestPreprocessDedupTemplateClustersAndTimeStatsAreStable(t *testing.T) {
	base := time.Date(2026, 7, 11, 8, 0, 15, 0, time.UTC)
	input := []model.LogItem{
		{Timestamp: base, Level: "error", Message: "request 123 failed for user 42", Pod: "api-0"},
		{Timestamp: base, Level: "ERROR", Message: "request 123 failed for user 42", Pod: "api-0"},
		{Timestamp: base.Add(20 * time.Second), Level: "warn", Message: "request 456 failed for user 99", Pod: "api-1"},
		{Timestamp: base.Add(2 * time.Minute), Level: "info", Message: "health check ok", Pod: "api-0"},
	}
	first := Preprocess(PreprocessInput{Items: input})
	second := Preprocess(PreprocessInput{Items: input})
	if first.TotalInput != 4 || first.TotalOutput != 3 {
		t.Fatalf("dedup result = %+v", first)
	}
	if len(first.Clusters) != 2 || first.Clusters[0].Template != "request <num> failed for user <num>" || first.Clusters[0].Count != 3 {
		t.Fatalf("clusters = %+v", first.Clusters)
	}
	if len(first.TimeStats) != 2 || first.TimeStats[0].Count != 3 || first.TimeStats[0].ErrorCount != 2 {
		t.Fatalf("time stats = %+v", first.TimeStats)
	}
	if first.Clusters[0] != second.Clusters[0] || first.Clusters[1] != second.Clusters[1] {
		t.Fatalf("clusters are not stable: first=%+v second=%+v", first.Clusters, second.Clusters)
	}
}

func TestPreprocessTruncatesStack(t *testing.T) {
	lines := []string{"panic: boom"}
	for i := 0; i < 10; i++ {
		lines = append(lines, "  at frame")
	}
	result := Preprocess(PreprocessInput{
		Items:         []model.LogItem{{Message: strings.Join(lines, "\n")}},
		StackMaxLines: 3,
	})
	if !strings.Contains(result.Items[0].Message, "[stack truncated]") {
		t.Fatalf("stack was not truncated: %q", result.Items[0].Message)
	}
}
