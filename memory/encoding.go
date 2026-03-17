/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

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

	embeddingStr := float32SliceToString(embedding)
	createQuery := fmt.Sprintf(`CREATE (e:Engram {
		id: '%s',
		content: '%s',
		summary: '%s',
		memory_type: '%s',
		importance: %f,
		access_count: 0,
		created_at: timestamp('%s'),
		last_accessed_at: timestamp('%s'),
		decay_factor: 1.0,
		embedding: %s,
		source: '%s',
		tags: '%s'
	})`,
		escapeCypher(engramID),
		escapeCypher(req.Content),
		escapeCypher(extraction.Summary),
		escapeCypher(extraction.MemoryType),
		importance,
		now.Format("2006-01-02 15:04:05"),
		now.Format("2006-01-02 15:04:05"),
		embeddingStr,
		escapeCypher(req.Source),
		escapeCypher(tags),
	)

	if err := e.store.Execute(createQuery); err != nil {
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

	checkQuery := fmt.Sprintf("MATCH (c:Cue {id: '%s'}) RETURN c.id", escapeCypher(cueID))
	rows, err := e.store.QueryRows(checkQuery)
	if err != nil {
		return fmt.Errorf("check cue existence: %w", err)
	}

	if len(rows) == 0 {
		cueEmbedding, err := e.llm.Embed(ctx, name)
		if err != nil {
			return fmt.Errorf("embed cue: %w", err)
		}
		embStr := float32SliceToString(cueEmbedding)

		createCue := fmt.Sprintf(`CREATE (c:Cue {
			id: '%s',
			name: '%s',
			cue_type: '%s',
			embedding: %s
		})`,
			escapeCypher(cueID),
			escapeCypher(name),
			escapeCypher(cueType),
			embStr,
		)
		if err := e.store.Execute(createCue); err != nil {
			return fmt.Errorf("create cue node: %w", err)
		}
	}

	linkQuery := fmt.Sprintf(`
		MATCH (e:Engram {id: '%s'}), (c:Cue {id: '%s'})
		CREATE (e)-[:EncodedBy {strength: 1.0, created_at: timestamp('%s')}]->(c)`,
		escapeCypher(engramID),
		escapeCypher(cueID),
		now.Format("2006-01-02 15:04:05"),
	)
	if err := e.store.Execute(linkQuery); err != nil {
		return fmt.Errorf("link engram to cue: %w", err)
	}

	return nil
}

func (e *Engine) autoAssociate(ctx context.Context, engramID string, embedding []float32) error {
	rows, err := e.store.QueryRows(
		fmt.Sprintf("MATCH (e:Engram) WHERE e.id <> '%s' RETURN e.id AS id, e.embedding AS emb", escapeCypher(engramID)),
	)
	if err != nil {
		return fmt.Errorf("query existing engrams: %w", err)
	}

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
			assocQuery := fmt.Sprintf(`
				MATCH (e1:Engram {id: '%s'}), (e2:Engram {id: '%s'})
				CREATE (e1)-[:AssociatedWith {relation_type: 'semantic', strength: %f, created_at: timestamp('%s')}]->(e2)`,
				escapeCypher(engramID),
				escapeCypher(otherID),
				sim,
				now.Format("2006-01-02 15:04:05"),
			)
			if err := e.store.Execute(assocQuery); err != nil {
				return fmt.Errorf("create association: %w", err)
			}
		}
	}
	return nil
}

func escapeCypher(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	return s
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
