// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const extractionSystemPrompt = `You are a memory encoding assistant. Given a piece of content, extract structured information for memory storage.

Respond ONLY with a valid JSON object (no markdown, no explanation) with these fields:
{
  "summary": "A concise 1-2 sentence summary of the content",
  "memory_type": "episodic|semantic|procedural",
  "entities": [
    {"name": "entity name", "type": "entity|keyword|concept|context"}
  ]
}

Rules:
- memory_type: "episodic" for events/experiences, "semantic" for facts/knowledge, "procedural" for how-to/processes
- Extract 3-10 entities that serve as retrieval cues
- Entity types: "entity" for named things (people, places, products), "keyword" for important terms, "concept" for abstract ideas, "context" for situational context
- Keep entity names lowercase and normalized
- Summary should capture the essential meaning for later retrieval`

const cueExtractionSystemPrompt = `You are a retrieval cue extractor. Given a search query, extract the key entities and concepts that should be used to search a memory system.

Respond ONLY with a valid JSON object (no markdown, no explanation, no prefixes and suffixes):
{
  "entities": ["entity1", "entity2", ...],
  "keywords": ["keyword1", "keyword2", ...]
}

Rules:
- Extract 1-5 specific entities (names, places, products, etc.)
- Extract 1-5 keywords that capture the intent
- Keep all values lowercase and normalized
- Focus on the most discriminative terms for retrieval`

type ExtractionResult struct {
	Summary    string            `json:"summary"`
	MemoryType string            `json:"memory_type"`
	Entities   []ExtractedEntity `json:"entities"`
}

type ExtractedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type CueExtractionResult struct {
	Entities []string `json:"entities"`
	Keywords []string `json:"keywords"`
}

func (c *Client) ExtractMemoryInfo(ctx context.Context, content string) (*ExtractionResult, error) {
	resp, err := c.ChatWithJSON(ctx, extractionSystemPrompt, content)
	if err != nil {
		return nil, fmt.Errorf("extract memory info: %w", err)
	}

	resp = cleanJSONResponse(resp)

	var result ExtractionResult
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return nil, fmt.Errorf("parse extraction result: %w (response: %s)", err, resp)
	}

	if result.MemoryType == "" {
		result.MemoryType = "semantic"
	}
	validTypes := map[string]bool{"episodic": true, "semantic": true, "procedural": true}
	if !validTypes[result.MemoryType] {
		result.MemoryType = "semantic"
	}

	for i := range result.Entities {
		result.Entities[i].Name = strings.ToLower(strings.TrimSpace(result.Entities[i].Name))
		if result.Entities[i].Type == "" {
			result.Entities[i].Type = "keyword"
		}
		validCueTypes := map[string]bool{"entity": true, "keyword": true, "concept": true, "context": true}
		if !validCueTypes[result.Entities[i].Type] {
			result.Entities[i].Type = "keyword"
		}
	}

	return &result, nil
}

func (c *Client) ExtractCues(ctx context.Context, query string) (*CueExtractionResult, error) {
	resp, err := c.ChatWithJSON(ctx, cueExtractionSystemPrompt, query)
	if err != nil {
		return nil, fmt.Errorf("extract cues: %w", err)
	}

	resp = cleanJSONResponse(resp)

	var result CueExtractionResult
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return nil, fmt.Errorf("parse cue extraction result: %w (response: %s)", err, resp)
	}

	for i := range result.Entities {
		result.Entities[i] = strings.ToLower(strings.TrimSpace(result.Entities[i]))
	}
	for i := range result.Keywords {
		result.Keywords[i] = strings.ToLower(strings.TrimSpace(result.Keywords[i]))
	}

	return &result, nil
}

func cleanJSONResponse(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}
