package rag

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
)

const (
	rerankCandidateLimit = 30
	defaultContextBudget = 6000
	maxDocumentShare     = 0.5
)

var dangerousContextCommand = regexp.MustCompile(`(?i)(?:^|\s)(rm\s+-rf|kubectl\s+delete|helm\s+uninstall|docker\s+rm|drop\s+(?:table|database)|truncate\s+table|systemctl\s+stop)\b`)

type RerankResultTrace struct {
	ChunkID        int64   `json:"chunkId"`
	Rank           int     `json:"rank"`
	Score          float64 `json:"score"`
	RelevanceLabel string  `json:"relevanceLabel"`
}

type RerankTrace struct {
	Model      string              `json:"model,omitempty"`
	InputCount int                 `json:"inputCount"`
	TopN       int                 `json:"topN"`
	Degraded   bool                `json:"degraded"`
	Error      string              `json:"error,omitempty"`
	Results    []RerankResultTrace `json:"results"`
}

type ContextBuildTrace struct {
	InputCount      int                 `json:"inputCount"`
	Deduplicated    int                 `json:"deduplicatedCount"`
	DocumentLimited int                 `json:"documentLimitedCount"`
	Merged          int                 `json:"mergedCount"`
	ParentExpanded  int                 `json:"parentExpandedCount"`
	TokenBudget     int                 `json:"tokenBudget"`
	TokensUsed      int                 `json:"tokensUsed"`
	Selected        int                 `json:"selectedCount"`
	Degraded        bool                `json:"degraded"`
	Error           string              `json:"error,omitempty"`
	Blocks          []ContextBlockTrace `json:"blocks"`
}

type ContextEvidenceTrace struct {
	ChunkID      int64         `json:"chunkId"`
	RRFScore     float64       `json:"rrfScore"`
	ChannelRanks []ChannelRank `json:"channelRanks"`
	RerankRank   int           `json:"rerankRank"`
	RerankScore  float64       `json:"rerankScore"`
}

type ContextBlockTrace struct {
	CitationID        string                 `json:"citationId"`
	DocumentID        int64                  `json:"documentId"`
	DocumentVersionID int64                  `json:"documentVersionId"`
	ChunkIDs          []int64                `json:"chunkIds"`
	RetrievalTrace    []ContextEvidenceTrace `json:"retrievalTrace"`
}

type ContextBlock struct {
	CitationID        string                 `json:"citationId"`
	DocumentID        int64                  `json:"documentId"`
	DocumentVersionID int64                  `json:"documentVersionId"`
	ChunkIDs          []int64                `json:"chunkIds"`
	ChunkIndex        int                    `json:"chunkIndex"`
	Title             *string                `json:"title,omitempty"`
	Section           *string                `json:"section,omitempty"`
	Content           string                 `json:"content"`
	Applicability     string                 `json:"applicability,omitempty"`
	ChunkType         string                 `json:"chunkType"`
	RetrievalTrace    []ContextEvidenceTrace `json:"retrievalTrace"`
}

func (s *Service) loadRetrievalDocuments(ctx context.Context, chunks []model.KBChunk) (map[int64]model.KBDocument, error) {
	ids := make([]int64, 0, len(chunks))
	seen := map[int64]struct{}{}
	for _, chunk := range chunks {
		if _, ok := seen[chunk.DocumentID]; ok {
			continue
		}
		seen[chunk.DocumentID] = struct{}{}
		ids = append(ids, chunk.DocumentID)
	}
	documents, err := s.repository.FindKnowledgeDocumentsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make(map[int64]model.KBDocument, len(documents))
	for _, document := range documents {
		result[document.ID] = document
	}
	return result, nil
}

func (s *Service) rerankCandidates(ctx context.Context, query string, chunks []model.KBChunk, documents map[int64]model.KBDocument, config *model.LLMConfig, credential modelCredential, ready bool) ([]model.KBChunk, RerankTrace) {
	if len(chunks) > rerankCandidateLimit {
		chunks = chunks[:rerankCandidateLimit]
	}
	trace := RerankTrace{InputCount: len(chunks), TopN: len(chunks)}
	if len(chunks) == 0 {
		return nil, trace
	}
	fallback, fallbackScores := metadataFallbackRank(query, chunks, documents)
	client, ok := s.client.(llmsvc.RerankClient)
	if !ready || config == nil || !ok {
		trace.Degraded = true
		trace.Error = "rerank model unavailable"
		trace.Results = rerankTraceResults(fallback, fallbackScores)
		return fallback, trace
	}
	trace.Model = config.Model
	documentsInput := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		document := documents[chunk.DocumentID]
		documentsInput = append(documentsInput, rerankDocument(chunk, document))
	}
	result, err := client.Rerank(ctx, llmsvc.RerankRequest{
		BaseURL: config.BaseURL, APIKey: credential.APIKey, APISecret: credential.APISecret,
		Model: config.Model, Query: query, Documents: documentsInput, TopN: len(chunks),
	})
	if err != nil || result == nil || len(result.Results) == 0 {
		trace.Degraded = true
		trace.Error = "rerank request failed"
		if err != nil {
			trace.Error = err.Error()
		}
		trace.Results = rerankTraceResults(fallback, fallbackScores)
		return fallback, trace
	}
	ranked := make([]model.KBChunk, 0, len(chunks))
	scores := map[int64]float64{}
	seen := map[int]struct{}{}
	for _, item := range result.Results {
		if item.Index < 0 || item.Index >= len(chunks) {
			continue
		}
		if _, exists := seen[item.Index]; exists {
			continue
		}
		seen[item.Index] = struct{}{}
		chunk := chunks[item.Index]
		ranked = append(ranked, chunk)
		scores[chunk.ID] = item.RelevanceScore
	}
	for index, chunk := range chunks {
		if _, exists := seen[index]; !exists {
			ranked = append(ranked, chunk)
		}
	}
	trace.Results = rerankTraceResults(ranked, scores)
	return ranked, trace
}

func rerankDocument(chunk model.KBChunk, document model.KBDocument) string {
	parts := []string{}
	if document.Title != "" {
		parts = append(parts, "Title: "+document.Title)
	} else if chunk.SourceTitle != nil {
		parts = append(parts, "Title: "+*chunk.SourceTitle)
	}
	if chunk.SourceSection != nil {
		parts = append(parts, "Section: "+*chunk.SourceSection)
	}
	for _, field := range []struct {
		label string
		value *string
	}{{"Document type", document.DocType}, {"System", document.SystemName}, {"Component", document.ComponentName}, {"Environment", document.Environment}} {
		label, value := field.label, field.value
		if value != nil && strings.TrimSpace(*value) != "" {
			parts = append(parts, label+": "+strings.TrimSpace(*value))
		}
	}
	parts = append(parts, "Content:\n"+chunk.Content)
	return strings.Join(parts, "\n")
}

func metadataFallbackRank(query string, chunks []model.KBChunk, documents map[int64]model.KBDocument) ([]model.KBChunk, map[int64]float64) {
	type scored struct {
		chunk model.KBChunk
		score float64
		order int
	}
	terms := tokenize(query)
	items := make([]scored, 0, len(chunks))
	for index, chunk := range chunks {
		document := documents[chunk.DocumentID]
		metadata := document.Title
		if chunk.SourceTitle != nil {
			metadata += " " + *chunk.SourceTitle
		}
		if chunk.SourceSection != nil {
			metadata += " " + *chunk.SourceSection
		}
		for _, value := range []*string{document.SystemName, document.ComponentName, document.Environment, document.DocType} {
			if value != nil {
				metadata += " " + *value
			}
		}
		score := 1 / float64(index+1)
		lower := strings.ToLower(metadata)
		for _, term := range terms {
			if strings.Contains(lower, term) {
				score += .25
			}
		}
		items = append(items, scored{chunk: chunk, score: score, order: index})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].order < items[j].order
		}
		return items[i].score > items[j].score
	})
	result := make([]model.KBChunk, 0, len(items))
	scores := make(map[int64]float64, len(items))
	for _, item := range items {
		result = append(result, item.chunk)
		scores[item.chunk.ID] = item.score
	}
	return result, scores
}

func rerankTraceResults(chunks []model.KBChunk, scores map[int64]float64) []RerankResultTrace {
	result := make([]RerankResultTrace, 0, len(chunks))
	for index, chunk := range chunks {
		score := 1 / float64(index+1)
		if scores != nil {
			if value, ok := scores[chunk.ID]; ok {
				score = value
			}
		}
		label := "low"
		if score >= .7 {
			label = "high"
		} else if score >= .4 {
			label = "medium"
		}
		result = append(result, RerankResultTrace{ChunkID: chunk.ID, Rank: index + 1, Score: score, RelevanceLabel: label})
	}
	return result
}

func (s *Service) buildContext(ctx context.Context, chunks []model.KBChunk, documents map[int64]model.KBDocument, evidence map[int64]ContextEvidenceTrace, limit, budget int) ([]ContextBlock, ContextBuildTrace) {
	if budget <= 0 {
		budget = defaultContextBudget
	}
	trace := ContextBuildTrace{InputCount: len(chunks), TokenBudget: budget}
	chunks = deduplicateSemanticChunks(chunks)
	trace.Deduplicated = len(chunks)
	chunks = limitDocumentShare(chunks, limit)
	trace.DocumentLimited = len(chunks)
	parentIDs := make([]int64, 0)
	for _, chunk := range chunks {
		if chunk.ParentChunkID != nil {
			parentIDs = append(parentIDs, *chunk.ParentChunkID)
		}
	}
	parents, parentErr := s.repository.FindKnowledgeChunksByIDs(ctx, parentIDs)
	if parentErr != nil {
		trace.Degraded = true
		trace.Error = parentErr.Error()
	}
	parentByID := make(map[int64]model.KBChunk, len(parents))
	for _, parent := range parents {
		parentByID[parent.ID] = parent
	}
	groups := mergeAdjacentChunks(chunks)
	trace.Merged = len(groups)
	blocks := make([]ContextBlock, 0, minInt(limit, len(groups)))
	used := 0
	for _, group := range groups {
		if len(blocks) >= limit || used >= budget {
			break
		}
		content, parentExpanded := buildGroupContent(group, parentByID)
		if parentExpanded {
			trace.ParentExpanded++
		}
		remaining := budget - used
		content = truncateContext(content, remaining)
		if strings.TrimSpace(content) == "" {
			continue
		}
		cost := contextTokenCount(content)
		first := group[0]
		document := documents[first.DocumentID]
		chunkIDs := make([]int64, 0, len(group))
		blockEvidence := make([]ContextEvidenceTrace, 0, len(group))
		for _, chunk := range group {
			chunkIDs = append(chunkIDs, chunk.ID)
			if item, ok := evidence[chunk.ID]; ok {
				blockEvidence = append(blockEvidence, item)
			}
		}
		citationID := fmt.Sprintf("KC-%d-%d", first.DocumentVersionID, chunkIDs[0])
		title := first.SourceTitle
		if document.Title != "" {
			title = stringPointer(document.Title)
		}
		blocks = append(blocks, ContextBlock{
			CitationID: citationID, DocumentID: first.DocumentID, DocumentVersionID: first.DocumentVersionID,
			ChunkIDs: chunkIDs, ChunkIndex: first.ChunkIndex, Title: title, Section: first.SourceSection, Content: content,
			Applicability: documentApplicability(document), ChunkType: first.ChunkType, RetrievalTrace: blockEvidence,
		})
		trace.Blocks = append(trace.Blocks, ContextBlockTrace{CitationID: citationID, DocumentID: first.DocumentID, DocumentVersionID: first.DocumentVersionID, ChunkIDs: append([]int64(nil), chunkIDs...), RetrievalTrace: blockEvidence})
		used += cost
	}
	trace.TokensUsed = used
	trace.Selected = len(blocks)
	return blocks, trace
}

func buildContextEvidence(retrieval RetrievalTrace, rerank RerankTrace) map[int64]ContextEvidenceTrace {
	result := make(map[int64]ContextEvidenceTrace, len(retrieval.Candidates))
	for _, candidate := range retrieval.Candidates {
		result[candidate.ChunkID] = ContextEvidenceTrace{ChunkID: candidate.ChunkID, RRFScore: candidate.RRFScore, ChannelRanks: append([]ChannelRank(nil), candidate.ChannelRanks...)}
	}
	for _, item := range rerank.Results {
		trace := result[item.ChunkID]
		trace.ChunkID = item.ChunkID
		trace.RerankRank = item.Rank
		trace.RerankScore = item.Score
		result[item.ChunkID] = trace
	}
	return result
}

func deduplicateSemanticChunks(chunks []model.KBChunk) []model.KBChunk {
	result := make([]model.KBChunk, 0, len(chunks))
	for _, chunk := range chunks {
		duplicate := false
		for _, existing := range result {
			if chunk.ID == existing.ID || (chunk.DocumentID == existing.DocumentID && contentSimilarity(chunk.Content, existing.Content) >= .85) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, chunk)
		}
	}
	return result
}

func contentSimilarity(left, right string) float64 {
	a, b := runeShingles(left, 3), runeShingles(right, 3)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for value := range a {
		if _, ok := b[value]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 1
	}
	return float64(intersection) / float64(union)
}

func runeShingles(value string, size int) map[string]struct{} {
	value = strings.ToLower(strings.TrimSpace(value))
	runes := []rune(value)
	result := map[string]struct{}{}
	if len(runes) < size {
		if value != "" {
			result[value] = struct{}{}
		}
		return result
	}
	for index := 0; index+size <= len(runes); index++ {
		result[string(runes[index:index+size])] = struct{}{}
	}
	return result
}

func limitDocumentShare(chunks []model.KBChunk, limit int) []model.KBChunk {
	if limit <= 0 {
		return nil
	}
	perDocument := int(float64(limit)*maxDocumentShare + .999)
	if perDocument < 1 {
		perDocument = 1
	}
	counts := map[int64]int{}
	result := make([]model.KBChunk, 0, limit)
	for _, chunk := range chunks {
		if counts[chunk.DocumentID] >= perDocument {
			continue
		}
		counts[chunk.DocumentID]++
		result = append(result, chunk)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func mergeAdjacentChunks(chunks []model.KBChunk) [][]model.KBChunk {
	used := make([]bool, len(chunks))
	groups := make([][]model.KBChunk, 0, len(chunks))
	for index, chunk := range chunks {
		if used[index] {
			continue
		}
		used[index] = true
		group := []model.KBChunk{chunk}
		changed := true
		for changed {
			changed = false
			for candidateIndex, candidate := range chunks {
				if used[candidateIndex] || !sameChunkSequence(chunk, candidate) {
					continue
				}
				for _, member := range group {
					if absInt(member.ChunkIndex-candidate.ChunkIndex) == 1 {
						used[candidateIndex] = true
						group = append(group, candidate)
						changed = true
						break
					}
				}
			}
		}
		sort.SliceStable(group, func(i, j int) bool { return group[i].ChunkIndex < group[j].ChunkIndex })
		groups = append(groups, group)
	}
	return groups
}

func sameChunkSequence(left, right model.KBChunk) bool {
	if left.DocumentVersionID != right.DocumentVersionID || left.StrategyID != right.StrategyID {
		return false
	}
	if left.SiblingGroup == nil || right.SiblingGroup == nil {
		return false
	}
	return *left.SiblingGroup == *right.SiblingGroup
}

func buildGroupContent(group []model.KBChunk, parents map[int64]model.KBChunk) (string, bool) {
	parts := []string{}
	parentExpanded := false
	seenParents := map[int64]struct{}{}
	for _, chunk := range group {
		if chunk.ParentChunkID != nil {
			if parent, ok := parents[*chunk.ParentChunkID]; ok {
				if _, seen := seenParents[parent.ID]; !seen {
					label := ""
					if parent.SourceSection != nil {
						label = *parent.SourceSection
					} else {
						label = firstLine(parent.Content)
					}
					if label != "" {
						parts = append(parts, "Parent: "+label)
						parentExpanded = true
					}
					seenParents[parent.ID] = struct{}{}
				}
			}
		}
		parts = append(parts, protectRiskContext(chunk))
	}
	return strings.Join(parts, "\n\n"), parentExpanded
}

func protectRiskContext(chunk model.KBChunk) string {
	content := strings.TrimSpace(chunk.Content)
	if !dangerousContextCommand.MatchString(content) {
		return content
	}
	lower := strings.ToLower(content)
	if strings.Contains(lower, "risk") || strings.Contains(lower, "warning") || strings.Contains(lower, "风险") || strings.Contains(lower, "警告") || strings.Contains(lower, "注意") {
		return content
	}
	risk := "执行前确认审批、影响范围和回滚方案。"
	if chunk.ContextBefore != nil && strings.TrimSpace(*chunk.ContextBefore) != "" {
		risk = strings.TrimSpace(*chunk.ContextBefore)
	}
	parts := []string{"Risk Context: " + risk, content}
	if chunk.ContextAfter != nil && strings.TrimSpace(*chunk.ContextAfter) != "" {
		parts = append(parts, "Verification / Rollback: "+strings.TrimSpace(*chunk.ContextAfter))
	}
	return strings.Join(parts, "\n\n")
}

func truncateContext(content string, budget int) string {
	if budget <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(content))
	if len(runes) <= budget {
		return string(runes)
	}
	if budget <= 3 {
		return string(runes[:budget])
	}
	return string(runes[:budget-3]) + "..."
}

func contextTokenCount(content string) int {
	count := utf8.RuneCountInString(content)
	if count < 1 {
		return 1
	}
	return count
}

func documentApplicability(document model.KBDocument) string {
	parts := []string{}
	for _, field := range []struct {
		label string
		value *string
	}{{"system", document.SystemName}, {"component", document.ComponentName}, {"environment", document.Environment}, {"docType", document.DocType}} {
		label, value := field.label, field.value
		if value != nil && strings.TrimSpace(*value) != "" {
			parts = append(parts, label+"="+strings.TrimSpace(*value))
		}
	}
	return strings.Join(parts, ", ")
}

func firstLine(value string) string {
	line := strings.TrimSpace(strings.SplitN(value, "\n", 2)[0])
	if len([]rune(line)) > 120 {
		return string([]rune(line)[:120])
	}
	return line
}

func stringPointer(value string) *string { return &value }
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
