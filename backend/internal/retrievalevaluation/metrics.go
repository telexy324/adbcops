package retrievalevaluation

import "math"

type RankingMetrics struct {
	RecallAtK float64 `json:"recallAtK"`
	MRR       float64 `json:"mrr"`
	NDCGAtK   float64 `json:"ndcgAtK"`
	HitRate   float64 `json:"hitRate"`
}

func CalculateRankingMetrics(expected, retrieved []int64, k int) RankingMetrics {
	if k <= 0 || k > len(retrieved) {
		k = len(retrieved)
	}
	relevant := make(map[int64]struct{}, len(expected))
	for _, id := range expected {
		if id > 0 {
			relevant[id] = struct{}{}
		}
	}
	if len(relevant) == 0 {
		return RankingMetrics{}
	}
	hits, reciprocal, dcg := 0, 0.0, 0.0
	seen := map[int64]struct{}{}
	for index, id := range retrieved[:k] {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := relevant[id]; !ok {
			continue
		}
		hits++
		if reciprocal == 0 {
			reciprocal = 1 / float64(index+1)
		}
		dcg += 1 / math.Log2(float64(index+2))
	}
	idealHits := len(relevant)
	if idealHits > k {
		idealHits = k
	}
	idcg := 0.0
	for index := 0; index < idealHits; index++ {
		idcg += 1 / math.Log2(float64(index+2))
	}
	ndcg := 0.0
	if idcg > 0 {
		ndcg = dcg / idcg
	}
	hitRate := 0.0
	if hits > 0 {
		hitRate = 1
	}
	return RankingMetrics{RecallAtK: float64(hits) / float64(len(relevant)), MRR: reciprocal, NDCGAtK: ndcg, HitRate: hitRate}
}

func CitationAccuracy(expected, cited []int64, expectNoAnswer bool) float64 {
	if len(cited) == 0 {
		if expectNoAnswer {
			return 1
		}
		return 0
	}
	relevant := map[int64]struct{}{}
	for _, id := range expected {
		if id > 0 {
			relevant[id] = struct{}{}
		}
	}
	correct := 0
	for _, id := range cited {
		if _, ok := relevant[id]; ok {
			correct++
		}
	}
	return float64(correct) / float64(len(cited))
}
