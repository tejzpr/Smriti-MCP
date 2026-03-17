/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func (e *Engine) Recall(ctx context.Context, req RecallRequest) ([]SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.1
	}

	cues, err := e.llm.ExtractCues(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("extract cues: %w", err)
	}

	queryEmbedding, err := e.llm.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	seen := make(map[string]*SearchResult)

	if err := e.fanOutCueSearch(cues.Entities, cues.Keywords, seen); err != nil {
		return nil, fmt.Errorf("cue search: %w", err)
	}

	if err := e.vectorSearch(queryEmbedding, limit*2, seen); err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	if err := e.multiHopExpand(seen, 2); err != nil {
		return nil, fmt.Errorf("multi-hop expand: %w", err)
	}

	e.scoreResults(seen, queryEmbedding)

	results := make([]SearchResult, 0, len(seen))
	for _, r := range seen {
		if r.Score >= minScore {
			results = append(results, *r)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}

	for _, r := range results {
		e.strengthenAccess(r.Engram.ID)
	}

	return results, nil
}

func (e *Engine) fanOutCueSearch(entities, keywords []string, seen map[string]*SearchResult) error {
	allCues := make([]string, 0, len(entities)+len(keywords))
	allCues = append(allCues, entities...)
	allCues = append(allCues, keywords...)

	for _, cueName := range allCues {
		rows, err := e.store.PreparedQueryRows(
			`MATCH (e:Engram)-[:EncodedBy]->(c:Cue)
			WHERE c.name = $cueName
			RETURN e.id AS id, e.content AS content, e.summary AS summary,
				e.memory_type AS memory_type, e.importance AS importance,
				e.access_count AS access_count, e.decay_factor AS decay_factor,
				e.embedding AS embedding, e.source AS source, e.tags AS tags,
				e.created_at AS created_at, e.last_accessed_at AS last_accessed_at`,
			map[string]any{"cueName": cueName},
		)
		if err != nil {
			continue
		}
		for _, row := range rows {
			eng := rowToEngram(row)
			if _, exists := seen[eng.ID]; !exists {
				seen[eng.ID] = &SearchResult{
					Engram:    eng,
					MatchType: "cue",
				}
			}
		}
	}
	return nil
}

func (e *Engine) vectorSearch(queryEmbedding []float32, limit int, seen map[string]*SearchResult) error {
	if e.tryHNSWSearch(queryEmbedding, limit, seen) {
		return nil
	}
	return e.vectorSearchFallback(queryEmbedding, limit, seen)
}

func (e *Engine) tryHNSWSearch(queryEmbedding []float32, limit int, seen map[string]*SearchResult) bool {
	embStr := float32SliceToString(queryEmbedding)
	query := fmt.Sprintf(`CALL QUERY_VECTOR_INDEX('Engram', 'engram_embedding_idx', %s, %d)
		RETURN node.id AS id, node.content AS content, node.summary AS summary,
			node.memory_type AS memory_type, node.importance AS importance,
			node.access_count AS access_count, node.decay_factor AS decay_factor,
			node.embedding AS embedding, node.source AS source, node.tags AS tags,
			node.created_at AS created_at, node.last_accessed_at AS last_accessed_at,
			distance
		ORDER BY distance`, embStr, limit)
	rows, err := e.store.QueryRows(query)
	if err != nil {
		return false
	}
	for _, row := range rows {
		eng := rowToEngram(row)
		if _, exists := seen[eng.ID]; !exists {
			dist := toFloat64(row["distance"])
			seen[eng.ID] = &SearchResult{
				Engram:    eng,
				Score:     1.0 - dist,
				MatchType: "vector",
			}
		}
	}
	return true
}

func (e *Engine) vectorSearchFallback(queryEmbedding []float32, limit int, seen map[string]*SearchResult) error {
	query := `MATCH (e:Engram)
		RETURN e.id AS id, e.content AS content, e.summary AS summary,
			e.memory_type AS memory_type, e.importance AS importance,
			e.access_count AS access_count, e.decay_factor AS decay_factor,
			e.embedding AS embedding, e.source AS source, e.tags AS tags,
			e.created_at AS created_at, e.last_accessed_at AS last_accessed_at`
	rows, err := e.store.QueryRows(query)
	if err != nil {
		return nil
	}

	type scored struct {
		eng Engram
		sim float64
	}
	var candidates []scored
	for _, row := range rows {
		eng := rowToEngram(row)
		emb := eng.Embedding
		if len(emb) == 0 {
			continue
		}
		sim := cosineSimilarity(queryEmbedding, emb)
		candidates = append(candidates, scored{eng: eng, sim: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].sim > candidates[j].sim
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	for _, c := range candidates {
		if _, exists := seen[c.eng.ID]; !exists {
			seen[c.eng.ID] = &SearchResult{
				Engram:    c.eng,
				MatchType: "vector",
			}
		}
	}
	return nil
}

func (e *Engine) multiHopExpand(seen map[string]*SearchResult, maxHops int) error {
	for hop := 1; hop <= maxHops; hop++ {
		currentIDs := make([]string, 0, len(seen))
		for id := range seen {
			currentIDs = append(currentIDs, id)
		}
		if len(currentIDs) == 0 {
			break
		}

		// Batch all candidate IDs into a single query per hop
		idList := make([]any, len(currentIDs))
		for i, id := range currentIDs {
			idList[i] = id
		}
		rows, err := e.store.PreparedQueryRows(
			`MATCH (e1:Engram)-[r:AssociatedWith]->(e2:Engram)
			WHERE e1.id IN $ids AND r.strength > 0.5
			RETURN e2.id AS id, e2.content AS content, e2.summary AS summary,
				e2.memory_type AS memory_type, e2.importance AS importance,
				e2.access_count AS access_count, e2.decay_factor AS decay_factor,
				e2.embedding AS embedding, e2.source AS source, e2.tags AS tags,
				e2.created_at AS created_at, e2.last_accessed_at AS last_accessed_at,
				r.strength AS strength`,
			map[string]any{"ids": idList},
		)
		if err != nil {
			continue
		}
		for _, row := range rows {
			eng := rowToEngram(row)
			if _, exists := seen[eng.ID]; !exists {
				seen[eng.ID] = &SearchResult{
					Engram:    eng,
					MatchType: "association",
					HopDepth:  hop,
				}
			}
		}
	}
	return nil
}

func (e *Engine) scoreResults(seen map[string]*SearchResult, queryEmbedding []float32) {
	now := time.Now()
	for _, r := range seen {
		sim := float64(0)
		emb := r.Engram.Embedding
		if len(emb) > 0 && len(queryEmbedding) > 0 {
			sim = cosineSimilarity(emb, queryEmbedding)
		}

		recency := recencyScore(r.Engram.LastAccessedAt, now)
		importance := r.Engram.Importance
		decay := r.Engram.DecayFactor

		hopPenalty := 1.0
		if r.HopDepth > 0 {
			hopPenalty = 1.0 / float64(1+r.HopDepth)
		}

		r.Score = (0.4*sim + 0.2*recency + 0.2*importance + 0.2*decay) * hopPenalty
	}
}

func (e *Engine) strengthenAccess(engramID string) {
	e.store.PreparedExecute(
		`MATCH (e:Engram {id: $eid})
		SET e.access_count = e.access_count + 1,
			e.last_accessed_at = timestamp($ts),
			e.decay_factor = CASE WHEN e.decay_factor + 0.05 > 1.0 THEN 1.0 ELSE e.decay_factor + 0.05 END`,
		map[string]any{"eid": engramID, "ts": time.Now().Format("2006-01-02 15:04:05")},
	)
}

func recencyScore(lastAccessed time.Time, now time.Time) float64 {
	hours := now.Sub(lastAccessed).Hours()
	if hours <= 0 {
		return 1.0
	}
	score := 1.0 / (1.0 + hours/24.0)
	return score
}

func rowToEngram(row map[string]any) Engram {
	eng := Engram{}
	if v, ok := row["id"].(string); ok {
		eng.ID = v
	}
	if v, ok := row["content"].(string); ok {
		eng.Content = v
	}
	if v, ok := row["summary"].(string); ok {
		eng.Summary = v
	}
	if v, ok := row["memory_type"].(string); ok {
		eng.MemoryType = v
	}
	if v, ok := row["importance"]; ok {
		eng.Importance = toFloat64(v)
	}
	if v, ok := row["access_count"]; ok {
		eng.AccessCount = toInt64(v)
	}
	if v, ok := row["decay_factor"]; ok {
		eng.DecayFactor = toFloat64(v)
	}
	eng.Embedding = extractEmbedding(row["embedding"])
	if v, ok := row["source"].(string); ok {
		eng.Source = v
	}
	if v, ok := row["tags"].(string); ok {
		eng.Tags = v
	}
	if v, ok := row["created_at"].(time.Time); ok {
		eng.CreatedAt = v
	}
	if v, ok := row["last_accessed_at"].(time.Time); ok {
		eng.LastAccessedAt = v
	}
	return eng
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	default:
		return 0
	}
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	default:
		return 0
	}
}
