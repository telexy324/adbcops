package qualityevaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	llmsvc "aiops-platform/backend/internal/llm"
	"aiops-platform/backend/internal/model"
)

func TestLLMResultWithoutEvidenceIsRejected(t *testing.T) {
	profile := llmProfile()
	client := &scriptedLLMClient{respond: func(request llmsvc.ChatRequest, _ int) (string, error) {
		return `{"criterionKey":"accuracy","ruleResults":[{"ruleKey":"semantic_consistency","score":8,"maxScore":10,"status":"present","confidence":0.9,"evidence":[]}]}`, nil
	}}
	output := EvaluateWithLLM(context.Background(), client, LLMRunConfig{Model: "test"}, profile, llmBlocks(1), pendingFor(profile))
	if len(output.Results) != 0 || !containsWarning(output.ValidationWarnings, "invalid or missing evidence") {
		t.Fatalf("evidence-free result accepted: %+v", output)
	}
}

func TestLLMResultWithUnknownBlockIsRejected(t *testing.T) {
	profile := llmProfile()
	client := &scriptedLLMClient{respond: func(request llmsvc.ChatRequest, _ int) (string, error) {
		return `{"criterionKey":"accuracy","ruleResults":[{"ruleKey":"semantic_consistency","score":8,"maxScore":10,"status":"present","confidence":0.9,"evidence":[{"blockId":"invented","quote":"text","reason":"evidence"}]}]}`, nil
	}}
	output := EvaluateWithLLM(context.Background(), client, LLMRunConfig{Model: "test"}, profile, llmBlocks(1), pendingFor(profile))
	if len(output.Results) != 0 {
		t.Fatalf("invented block accepted: %+v", output.Results)
	}
}

func TestLongDocumentUsesMapReduceAndOutputsCriterionScore(t *testing.T) {
	profile := llmProfile()
	client := &scriptedLLMClient{respond: validBatchResponse}
	output := EvaluateWithLLM(context.Background(), client, LLMRunConfig{Model: "test"}, profile, llmBlocks(25), pendingFor(profile))
	if output.Calls < 4 || client.calls != output.Calls {
		t.Fatalf("long document was not batched: calls=%d", output.Calls)
	}
	if len(output.Results) != 1 || output.Results[0].Score == nil || *output.Results[0].Score != 8 {
		t.Fatalf("map results were not reduced: %+v", output.Results)
	}
	score := output.CriterionScores["accuracy"]
	if score.Score != 8 || score.MaxScore != 10 {
		t.Fatalf("criterion score missing: %+v", output.CriterionScores)
	}
	var evidence []Evidence
	if json.Unmarshal(output.Results[0].Evidence, &evidence) != nil || len(evidence) < 3 {
		t.Fatalf("map evidence was not reduced: %s", output.Results[0].Evidence)
	}
	if !strings.Contains(client.lastSystem, "cross-section consistency") {
		t.Fatalf("cross-section instruction missing: %s", client.lastSystem)
	}
}

func TestLLMFailurePreservesDeterministicResults(t *testing.T) {
	profile := llmProfile()
	deterministic := EvaluateDeterministic(EngineInput{Profile: profile, Version: version(), Blocks: llmBlocks(1)})
	client := &scriptedLLMClient{respond: func(llmsvc.ChatRequest, int) (string, error) { return "", errors.New("model unavailable") }}
	llmOutput := EvaluateWithLLM(context.Background(), client, LLMRunConfig{Model: "test"}, profile, llmBlocks(1), pendingRuleKeys(deterministic.Results))
	merged, _ := mergeLLMResults(profile, deterministic, llmOutput.Results)
	if llmOutput.FailedCalls != 1 || len(merged.Results) != len(deterministic.Results) {
		t.Fatalf("deterministic fallback lost results: llm=%+v merged=%+v", llmOutput, merged)
	}
	if merged.Results[0].Score != nil || status(merged.Results[0]) != FindingManualConfirmationRequired {
		t.Fatalf("failed LLM fabricated score: %+v", merged.Results[0])
	}
}

func TestPromptRedactsCredentialBlocks(t *testing.T) {
	profile := llmProfile()
	blocks := []model.KBDocumentBlock{block("secret", 1, "version password=RealSecretValue")}
	client := &scriptedLLMClient{respond: func(request llmsvc.ChatRequest, _ int) (string, error) {
		if strings.Contains(request.Messages[1].Content, "RealSecretValue") {
			t.Fatal("credential leaked to LLM prompt")
		}
		return `{"criterionKey":"accuracy","ruleResults":[]}`, nil
	}}
	EvaluateWithLLM(context.Background(), client, LLMRunConfig{Model: "test"}, profile, blocks, pendingFor(profile))
}

func validBatchResponse(request llmsvc.ChatRequest, _ int) (string, error) {
	var payload struct {
		Blocks []struct {
			BlockID string `json:"blockId"`
			Text    string `json:"text"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil || len(payload.Blocks) == 0 {
		return "", fmt.Errorf("decode prompt: %w", err)
	}
	value := map[string]any{"criterionKey": "accuracy", "ruleResults": []any{map[string]any{"ruleKey": "semantic_consistency", "score": 8, "maxScore": 10, "status": "partial", "confidence": .9, "evidence": []any{map[string]any{"blockId": payload.Blocks[0].BlockID, "quote": payload.Blocks[0].Text, "reason": "supported by this block"}}, "deductionReason": "minor ambiguity", "suggestion": "clarify version scope"}}}
	data, _ := json.Marshal(value)
	return string(data), nil
}

func llmProfile() *model.KBQualityProfile {
	return &model.KBQualityProfile{ID: 1, TotalScore: 10, PassScore: 8, WarningScore: 7, Criteria: []model.KBQualityCriterion{{CriterionKey: "accuracy", Name: "Accuracy", ScoringMethod: "llm", MaxScore: 10, Weight: 1, Rules: []model.KBQualityRule{{RuleKey: "semantic_consistency", Name: "Version consistency", RuleType: "semantic", Severity: "high", MaxScore: 10, OrderNo: 1}}}}}
}

func llmBlocks(count int) []model.KBDocumentBlock {
	values := make([]model.KBDocumentBlock, 0, count)
	for index := 0; index < count; index++ {
		values = append(values, block(fmt.Sprintf("b-%02d", index+1), index+1, fmt.Sprintf("Version v%d applies to production parameter p%d", index+1, index+1)))
	}
	return values
}
func pendingFor(profile *model.KBQualityProfile) map[string]struct{} {
	return map[string]struct{}{profile.Criteria[0].CriterionKey + "\x00" + profile.Criteria[0].Rules[0].RuleKey: {}}
}
func containsWarning(values []string, expected string) bool {
	for _, value := range values {
		if strings.Contains(value, expected) {
			return true
		}
	}
	return false
}

type scriptedLLMClient struct {
	calls      int
	lastSystem string
	respond    func(llmsvc.ChatRequest, int) (string, error)
}

func (c *scriptedLLMClient) Chat(_ context.Context, request llmsvc.ChatRequest) (*llmsvc.ChatResult, error) {
	c.calls++
	c.lastSystem = request.Messages[0].Content
	content, err := c.respond(request, c.calls)
	if err != nil {
		return nil, err
	}
	return &llmsvc.ChatResult{Content: content, Model: request.Model}, nil
}
