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
)

func (e *Engine) Search(ctx context.Context, req RecallRequest) ([]SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	switch req.Mode {
	case "list":
		return e.listEngrams(limit)
	case "vector":
		return e.vectorOnlySearch(ctx, req.Query, limit)
	case "fts":
		return e.ftsSearch(req.Query, limit)
	default:
		return e.hybridSearch(ctx, req.Query, limit)
	}
}

func (e *Engine) listEngrams(limit int) ([]SearchResult, error) {
	query := fmt.Sprintf(`
		MATCH (e:Engram)`+tenantFilter(e.store, "e")+`
		RETURN %s
		ORDER BY e.last_accessed_at DESC
		LIMIT %d`, engramCols(e.store, "e"), limit)

	var rows []map[string]any
	var err error
	if isTenant(e.store) {
		rows, err = e.store.PreparedQueryRows(query, tenantParam(e.store, nil))
	} else {
		rows, err = e.store.QueryRows(query)
	}
	if err != nil {
		return nil, fmt.Errorf("list engrams: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		eng := rowToEngram(row)
		results = append(results, SearchResult{
			Engram:    eng,
			Score:     eng.Importance,
			MatchType: "list",
		})
	}
	return results, nil
}

func (e *Engine) vectorOnlySearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	queryEmbedding, err := e.llm.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	if results := e.tryHNSWSearchDirect(queryEmbedding, limit); results != nil {
		return results, nil
	}

	return e.vectorSearchFallbackDirect(queryEmbedding, limit)
}

func (e *Engine) tryHNSWSearchDirect(queryEmbedding []float32, limit int) []SearchResult {
	embStr := float32SliceToString(queryEmbedding)
	q := vectorSearchQuery(e.store, "engram_embedding_idx", embStr, limit)
	rows, err := e.store.QueryRows(q)
	if err != nil {
		return nil
	}
	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		eng := rowToEngram(row)
		dist := toFloat64(row["distance"])
		results = append(results, SearchResult{
			Engram:    eng,
			Score:     1.0 - dist,
			MatchType: "vector",
		})
	}
	return results
}

func (e *Engine) vectorSearchFallbackDirect(queryEmbedding []float32, limit int) ([]SearchResult, error) {
	q := `MATCH (e:Engram)` + tenantFilter(e.store, "e") + `
		RETURN ` + engramCols(e.store, "e")

	var rows []map[string]any
	var err error
	if isTenant(e.store) {
		rows, err = e.store.PreparedQueryRows(q, tenantParam(e.store, nil))
	} else {
		rows, err = e.store.QueryRows(q)
	}
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
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

	results := make([]SearchResult, 0, len(candidates))
	for _, c := range candidates {
		results = append(results, SearchResult{
			Engram:    c.eng,
			Score:     c.sim,
			MatchType: "vector",
		})
	}
	return results, nil
}

func (e *Engine) ftsSearch(query string, limit int) ([]SearchResult, error) {
	var rows []map[string]any
	var err error
	switch e.store.DBType() {
	case "neo4j", "falkordb":
		q := ftsSearchQuery(e.store, "engram_fts_idx", query, limit)
		rows, err = e.store.QueryRows(q)
	default:
		rows, err = e.store.PreparedQueryRows(
			fmt.Sprintf(`CALL query_fts_index('Engram', 'engram_fts_idx', $q, top_k := %d)
			RETURN %s,
				score`, limit, engramCols(e.store, "node")),
			map[string]any{"q": query},
		)
	}
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		eng := rowToEngram(row)
		score := toFloat64(row["score"])
		results = append(results, SearchResult{
			Engram:    eng,
			Score:     score,
			MatchType: "fts",
		})
	}
	return results, nil
}

func (e *Engine) hybridSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	seen := make(map[string]*SearchResult)

	vectorResults, err := e.vectorOnlySearch(ctx, query, limit*2)
	if err == nil {
		for _, r := range vectorResults {
			r := r
			seen[r.Engram.ID] = &r
		}
	}

	ftsResults, err := e.ftsSearch(query, limit*2)
	if err == nil {
		for _, r := range ftsResults {
			if existing, ok := seen[r.Engram.ID]; ok {
				existing.Score = (existing.Score + r.Score) / 2
				existing.MatchType = "hybrid"
			} else {
				r := r
				seen[r.Engram.ID] = &r
			}
		}
	}

	results := make([]SearchResult, 0, len(seen))
	for _, r := range seen {
		results = append(results, *r)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (e *Engine) Forget(engramID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	params := tenantParam(e.store, map[string]any{"eid": engramID})
	e.store.PreparedExecute(
		`MATCH (e:Engram {id: $eid})-[r:EncodedBy]->() DELETE r`, params)
	e.store.PreparedExecute(
		`MATCH (e:Engram {id: $eid})-[r:AssociatedWith]->() DELETE r`, params)
	e.store.PreparedExecute(
		`MATCH ()-[r:AssociatedWith]->(e:Engram {id: $eid}) DELETE r`, params)
	if err := e.store.PreparedExecute(
		`MATCH (e:Engram {id: $eid}) DELETE e`, params,
	); err != nil {
		return fmt.Errorf("delete engram %s: %w", engramID, err)
	}

	return nil
}
