package retrievalevaluation

import (
	"math"
	"testing"
)

func TestCalculateRankingMetrics(t *testing.T) {
	metrics := CalculateRankingMetrics([]int64{1, 2}, []int64{3, 2, 1}, 3)
	if metrics.RecallAtK != 1 || metrics.MRR != .5 || metrics.HitRate != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
	wantNDCG := (1/math.Log2(3) + 1/math.Log2(4)) / (1/math.Log2(2) + 1/math.Log2(3))
	if math.Abs(metrics.NDCGAtK-wantNDCG) > 1e-12 {
		t.Fatalf("nDCG=%v want=%v", metrics.NDCGAtK, wantNDCG)
	}
}

func TestCitationAccuracy(t *testing.T) {
	if got := CitationAccuracy([]int64{1, 2}, []int64{1, 3}, false); got != .5 {
		t.Fatalf("accuracy=%v", got)
	}
	if got := CitationAccuracy(nil, nil, true); got != 1 {
		t.Fatalf("no-answer accuracy=%v", got)
	}
}

func TestSectionMetrics(t *testing.T) {
	metrics, accuracy := sectionMetrics([]string{"故障处理", "回滚"}, []string{"数据库 / 故障处理", "其他"}, 2)
	if metrics.RecallAtK != .5 || metrics.MRR != 1 || accuracy != .5 {
		t.Fatalf("metrics=%+v accuracy=%v", metrics, accuracy)
	}
}
