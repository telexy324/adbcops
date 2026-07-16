package rag

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
	"aiops-platform/backend/internal/repository"
)

const (
	defaultRRFK           = 60
	denseChannelBudget    = 40
	lexicalChannelBudget  = 40
	exactChannelBudget    = 20
	mergedCandidateBudget = 60
)

type QueryUnderstanding struct {
	NormalizedQuery string   `json:"normalizedQuery"`
	Keywords        []string `json:"keywords"`
	Entities        []string `json:"entities"`
	SystemName      string   `json:"systemName"`
	ComponentName   string   `json:"componentName"`
	Environment     string   `json:"environment"`
	DocTypes        []string `json:"docTypes"`
	TimeSensitivity string   `json:"timeSensitivity"`
	MustHaveTerms   []string `json:"mustHaveTerms"`
	NegativeTerms   []string `json:"negativeTerms"`
}

type ChannelTrace struct {
	Channel  string `json:"channel"`
	Count    int    `json:"count"`
	Degraded bool   `json:"degraded"`
	Error    string `json:"error,omitempty"`
}

type ChannelRank struct {
	Channel  string  `json:"channel"`
	Rank     int     `json:"rank"`
	RawScore float64 `json:"rawScore"`
}

type RetrievalCandidateTrace struct {
	ChunkID      int64         `json:"chunkId"`
	RRFScore     float64       `json:"rrfScore"`
	ChannelRanks []ChannelRank `json:"channelRanks"`
}

type RetrievalTrace struct {
	Understanding QueryUnderstanding                  `json:"queryUnderstanding"`
	Filters       repository.KnowledgeRetrievalFilter `json:"filters"`
	RRFK          int                                 `json:"rrfK"`
	Channels      []ChannelTrace                      `json:"channels"`
	Candidates    []RetrievalCandidateTrace           `json:"candidates"`
}

type rankedCandidate struct {
	Chunk model.KBChunk
	Score float64
	Ranks []ChannelRank
}

func (s *Service) understandQuery(ctx context.Context, question string, config *model.LLMConfig, credential modelCredential, ready bool) QueryUnderstanding {
	fallback := localQueryUnderstanding(question)
	if !ready || config == nil {
		return fallback
	}
	result, err := s.client.Chat(ctx, llmsvc.ChatRequest{
		BaseURL: config.BaseURL, Provider: config.Provider, APIKey: credential.APIKey,
		AppKey: credential.AppKey, APISecret: credential.APISecret, Model: config.Model, Temperature: 0,
		Messages: []llmsvc.ChatMessage{
			{Role: model.MessageRoleSystem, Content: `Analyze the knowledge search query. Return only valid JSON with keys normalizedQuery, keywords, entities, systemName, componentName, environment, docTypes, timeSensitivity, mustHaveTerms, negativeTerms. Use empty strings or arrays when unknown.`},
			{Role: model.MessageRoleUser, Content: question},
		},
	})
	if err != nil {
		return fallback
	}
	raw := strings.TrimSpace(result.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	var understood QueryUnderstanding
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &understood) != nil || strings.TrimSpace(understood.NormalizedQuery) == "" {
		return fallback
	}
	understood.NormalizedQuery = ruleBasedRewrite(understood.NormalizedQuery)
	understood.Keywords = cleanTerms(understood.Keywords)
	understood.MustHaveTerms = cleanTerms(understood.MustHaveTerms)
	understood.NegativeTerms = cleanTerms(understood.NegativeTerms)
	return understood
}

func localQueryUnderstanding(question string) QueryUnderstanding {
	normalized := ruleBasedRewrite(question)
	understood := QueryUnderstanding{NormalizedQuery: normalized, Keywords: tokenize(normalized)}
	lower := strings.ToLower(normalized)
	switch {
	case strings.Contains(lower, "生产") || strings.Contains(lower, "prod") || strings.Contains(lower, "production"):
		understood.Environment = "prod"
	case strings.Contains(lower, "预发") || strings.Contains(lower, "stage") || strings.Contains(lower, "staging"):
		understood.Environment = "staging"
	case strings.Contains(lower, "测试") || strings.Contains(lower, "test"):
		understood.Environment = "test"
	case strings.Contains(lower, "开发") || strings.Contains(lower, "dev"):
		understood.Environment = "dev"
	}
	for _, mapping := range []struct{ term, docType string }{
		{"故障", "incident_report"}, {"事故", "incident_report"}, {"runbook", "runbook"},
		{"操作手册", "runbook"}, {"架构", "architecture"}, {"faq", "faq"},
	} {
		if strings.Contains(lower, mapping.term) {
			understood.DocTypes = append(understood.DocTypes, mapping.docType)
		}
	}
	return understood
}

func cleanTerms(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *Service) hybridRetrieve(ctx context.Context, understood QueryUnderstanding, embeddingConfig *model.LLMConfig, credential modelCredential, embeddingReady bool) ([]model.KBChunk, RetrievalTrace) {
	filter := repository.KnowledgeRetrievalFilter{
		PermissionScope: "authenticated_published",
		SystemName:      understood.SystemName, ComponentName: understood.ComponentName,
		Environment: understood.Environment, DocTypes: understood.DocTypes,
		MustHaveTerms: understood.MustHaveTerms, NegativeTerms: understood.NegativeTerms, Now: time.Now().UTC(),
	}
	trace := RetrievalTrace{Understanding: understood, Filters: filter, RRFK: defaultRRFK}
	trace.Channels = append(trace.Channels, ChannelTrace{Channel: "metadata_filter"})
	channels := make(map[string][]repository.RankedKnowledgeChunk)
	run := func(name string, search func() ([]repository.RankedKnowledgeChunk, error)) {
		items, err := search()
		channel := ChannelTrace{Channel: name, Count: len(items)}
		if err != nil {
			channel.Degraded = true
			channel.Error = err.Error()
		} else {
			channels[name] = items
		}
		trace.Channels = append(trace.Channels, channel)
	}

	query := understood.NormalizedQuery
	run("pg_trgm", func() ([]repository.RankedKnowledgeChunk, error) {
		return s.repository.SearchChunksTrigram(ctx, query, filter, lexicalChannelBudget)
	})
	exactTerms := understood.MustHaveTerms
	if len(exactTerms) == 0 {
		exactTerms = understood.Keywords
	}
	run("exact_keyword", func() ([]repository.RankedKnowledgeChunk, error) {
		return s.repository.SearchChunksExact(ctx, exactTerms, filter, exactChannelBudget)
	})
	run("title_section", func() ([]repository.RankedKnowledgeChunk, error) {
		return s.repository.SearchChunksTitleSection(ctx, query, filter, exactChannelBudget)
	})
	run("possible_question", func() ([]repository.RankedKnowledgeChunk, error) {
		return s.repository.SearchChunksPossibleQuestions(ctx, query, filter, exactChannelBudget)
	})
	if embeddingReady && embeddingConfig != nil {
		client := s.client.(llmsvc.EmbeddingClient)
		result, err := client.Embed(ctx, llmsvc.EmbeddingRequest{
			BaseURL: embeddingConfig.BaseURL, APIKey: credential.APIKey, APISecret: credential.APISecret,
			Model: embeddingConfig.Model, Input: []string{query},
		})
		if err != nil || result == nil || len(result.Embeddings) != 1 || len(result.Embeddings[0]) == 0 {
			channel := ChannelTrace{Channel: "dense_vector", Degraded: true, Error: "query embedding unavailable"}
			if err != nil {
				channel.Error = err.Error()
			}
			trace.Channels = append(trace.Channels, channel)
		} else {
			run("dense_vector", func() ([]repository.RankedKnowledgeChunk, error) {
				return s.repository.SearchChunksDense(ctx, result.Embeddings[0], embeddingConfig.ID, embeddingConfig.Model, filter, denseChannelBudget)
			})
		}
	} else {
		trace.Channels = append(trace.Channels, ChannelTrace{Channel: "dense_vector", Degraded: true, Error: "embedding model unavailable"})
	}

	fused := fuseRRF(channels, defaultRRFK, mergedCandidateBudget)
	chunks := make([]model.KBChunk, 0, len(fused))
	trace.Candidates = make([]RetrievalCandidateTrace, 0, len(fused))
	for _, candidate := range fused {
		chunks = append(chunks, candidate.Chunk)
		trace.Candidates = append(trace.Candidates, RetrievalCandidateTrace{ChunkID: candidate.Chunk.ID, RRFScore: candidate.Score, ChannelRanks: candidate.Ranks})
	}
	return chunks, trace
}

func fuseRRF(channels map[string][]repository.RankedKnowledgeChunk, k, limit int) []rankedCandidate {
	if k <= 0 {
		k = defaultRRFK
	}
	byID := map[int64]*rankedCandidate{}
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		seen := map[int64]struct{}{}
		for index, item := range channels[name] {
			if item.Chunk.ID == 0 {
				continue
			}
			if _, ok := seen[item.Chunk.ID]; ok {
				continue
			}
			seen[item.Chunk.ID] = struct{}{}
			rank := index + 1
			candidate := byID[item.Chunk.ID]
			if candidate == nil {
				candidate = &rankedCandidate{Chunk: item.Chunk}
				byID[item.Chunk.ID] = candidate
			}
			candidate.Score += 1 / float64(k+rank)
			candidate.Ranks = append(candidate.Ranks, ChannelRank{Channel: name, Rank: rank, RawScore: item.Score})
		}
	}
	result := make([]rankedCandidate, 0, len(byID))
	for _, candidate := range byID {
		result = append(result, *candidate)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Chunk.ID < result[j].Chunk.ID
		}
		return result[i].Score > result[j].Score
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}
