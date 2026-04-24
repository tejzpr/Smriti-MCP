// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (e *Engine) Encode(ctx context.Context, req StoreRequest) (*Engram, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	extraction, err := e.llm.ExtractMemoryInfo(ctx, req.Content)
	if err != nil {
		return nil, fmt.Errorf("extract memory info: %w", err)
	}

	embedding, err := e.llm.Embed(ctx, req.Content)
	if err != nil {
		return nil, fmt.Errorf("embed content: %w", err)
	}

	now := time.Now()
	engramID := uuid.New().String()
	importance := req.Importance
	if importance <= 0 {
		importance = 0.5
	}

	tags := ""
	if len(req.Tags) > 0 {
		tags = strings.Join(req.Tags, ",")
	}

	engram := &Engram{
		ID:             engramID,
		Content:        req.Content,
		Summary:        extraction.Summary,
		MemoryType:     extraction.MemoryType,
		Importance:     importance,
		AccessCount:    0,
		CreatedAt:      now,
		LastAccessedAt: now,
		DecayFactor:    1.0,
		Embedding:      embedding,
		Source:         req.Source,
		Tags:           tags,
	}

	embeddingStr := embeddingLiteral(e.store, float32SliceToString(embedding))
	tsFn := tsFunc(e.store)
	tp := tenantProp(e.store)
	createQuery := fmt.Sprintf(`CREATE (e:Engram {
		%sid: $id,
		content: $content,
		summary: $summary,
		memory_type: $memtype,
		importance: $importance,
		access_count: 0,
		created_at: %s($ts),
		last_accessed_at: %s($ts),
		decay_factor: 1.0,
		embedding: %s,
		source: $source,
		tags: $tags,
		cluster_id: -1
	})`, tp, tsFn, tsFn, embeddingStr)

	if err := e.store.PreparedExecute(createQuery, tenantParam(e.store, map[string]any{
		"id":         engramID,
		"content":    req.Content,
		"summary":    extraction.Summary,
		"memtype":    extraction.MemoryType,
		"importance": importance,
		"ts":         now.Format(tsFormat(e.store)),
		"source":     req.Source,
		"tags":       tags,
	})); err != nil {
		return nil, fmt.Errorf("create engram: %w", err)
	}

	for _, entity := range extraction.Entities {
		if err := e.createOrGetCue(ctx, entity.Name, entity.Type, engramID, now); err != nil {
			return nil, fmt.Errorf("create cue %q: %w", entity.Name, err)
		}
	}

	if err := e.autoAssociate(ctx, engramID, embedding); err != nil {
		return nil, fmt.Errorf("auto-associate: %w", err)
	}

	return engram, nil
}

func (e *Engine) createOrGetCue(ctx context.Context, name, cueType, engramID string, now time.Time) error {
	cueID := fmt.Sprintf("cue-%s-%s", cueType, name)
	cueID = strings.ReplaceAll(cueID, " ", "-")
	if isTenant(e.store) {
		cueID = e.store.TenantUser() + ":" + cueID
	}

	rows, err := e.store.PreparedQueryRows(
		"MATCH (c:Cue {id: $cueID})"+tenantFilter(e.store, "c")+" RETURN c.id",
		tenantParam(e.store, map[string]any{"cueID": cueID}),
	)
	if err != nil {
		return fmt.Errorf("check cue existence: %w", err)
	}

	if len(rows) == 0 {
		cueEmbedding, err := e.llm.Embed(ctx, name)
		if err != nil {
			return fmt.Errorf("embed cue: %w", err)
		}
		embStr := embeddingLiteral(e.store, float32SliceToString(cueEmbedding))

		tp := tenantProp(e.store)
		createCue := fmt.Sprintf(`CREATE (c:Cue {
			%sid: $cueID,
			name: $name,
			cue_type: $cueType,
			embedding: %s
		})`, tp, embStr)
		if err := e.store.PreparedExecute(createCue, tenantParam(e.store, map[string]any{
			"cueID":   cueID,
			"name":    name,
			"cueType": cueType,
		})); err != nil {
			return fmt.Errorf("create cue node: %w", err)
		}
	}

	tsStr := now.Format(tsFormat(e.store))
	tsFn := tsFunc(e.store)
	if err := e.store.PreparedExecute(
		fmt.Sprintf(`MATCH (e:Engram {id: $eid}), (c:Cue {id: $cid})
		CREATE (e)-[:EncodedBy {strength: 1.0, created_at: %s($ts)}]->(c)`, tsFn),
		map[string]any{"eid": engramID, "cid": cueID, "ts": tsStr},
	); err != nil {
		return fmt.Errorf("link engram to cue: %w", err)
	}

	return nil
}

func (e *Engine) autoAssociate(ctx context.Context, engramID string, embedding []float32) error {
	rows, err := e.store.PreparedQueryRows(
		"MATCH (e:Engram) WHERE e.id <> $eid"+tenantFilterAnd(e.store, "e")+" RETURN e.id AS id, e.embedding AS emb",
		tenantParam(e.store, map[string]any{"eid": engramID}),
	)
	if err != nil {
		return fmt.Errorf("query existing engrams: %w", err)
	}

	var strongestID string
	var strongestSim float64

	tsFn := tsFunc(e.store)
	for _, row := range rows {
		otherID, ok := row["id"].(string)
		if !ok {
			continue
		}
		otherEmb := extractEmbedding(row["emb"])
		if otherEmb == nil {
			continue
		}

		sim := cosineSimilarity(embedding, otherEmb)
		if sim > 0.7 {
			now := time.Now()
			if err := e.store.PreparedExecute(
				fmt.Sprintf(`MATCH (e1:Engram {id: $eid1}), (e2:Engram {id: $eid2})
				CREATE (e1)-[:AssociatedWith {relation_type: 'semantic', strength: $str, created_at: %s($ts)}]->(e2)`, tsFn),
				map[string]any{
					"eid1": engramID,
					"eid2": otherID,
					"str":  sim,
					"ts":   now.Format(tsFormat(e.store)),
				},
			); err != nil {
				return fmt.Errorf("create association: %w", err)
			}
			if sim > strongestSim {
				strongestSim = sim
				strongestID = otherID
			}
		}
	}

	if strongestID != "" {
		cRows, err := e.store.PreparedQueryRows(
			`MATCH (e:Engram {id: $eid}) RETURN e.cluster_id AS cluster_id`,
			tenantParam(e.store, map[string]any{"eid": strongestID}),
		)
		if err == nil && len(cRows) > 0 {
			cid := toInt64(cRows[0]["cluster_id"])
			if cid >= 0 {
				_ = e.store.PreparedExecute(
					`MATCH (e:Engram {id: $eid})`+tenantFilter(e.store, "e")+` SET e.cluster_id = $cid`,
					tenantParam(e.store, map[string]any{"eid": engramID, "cid": cid}),
				)
			}
		}
	}
	return nil
}

func float32SliceToString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func extractEmbedding(val any) []float32 {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case []float32:
		return v
	case []float64:
		result := make([]float32, len(v))
		for i, f := range v {
			result[i] = float32(f)
		}
		return result
	case []any:
		result := make([]float32, 0, len(v))
		for _, item := range v {
			switch f := item.(type) {
			case float32:
				result = append(result, f)
			case float64:
				result = append(result, float32(f))
			default:
				return nil
			}
		}
		return result
	default:
		return nil
	}
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 100; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}
