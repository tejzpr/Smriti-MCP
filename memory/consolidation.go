/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/tejzpr/smriti-mcp/db"
)

const (
	decayRate      = 0.01
	pruneThreshold = 0.05
)

func (e *Engine) Consolidate(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.applyDecay(); err != nil {
		return fmt.Errorf("apply decay: %w", err)
	}

	if err := e.pruneWeak(); err != nil {
		return fmt.Errorf("prune weak: %w", err)
	}

	if err := e.strengthenFrequent(); err != nil {
		return fmt.Errorf("strengthen frequent: %w", err)
	}

	if err := e.cleanOrphanedCues(); err != nil {
		return fmt.Errorf("clean orphaned cues: %w", err)
	}

	db.EnsureIndexes(e.store)

	return nil
}

func (e *Engine) applyDecay() error {
	rows, err := e.store.QueryRows(`
		MATCH (e:Engram)
		RETURN e.id AS id, e.decay_factor AS decay_factor,
			e.last_accessed_at AS last_accessed_at`)
	if err != nil {
		return fmt.Errorf("query engrams for decay: %w", err)
	}

	now := time.Now()
	for _, row := range rows {
		id, ok := row["id"].(string)
		if !ok {
			continue
		}
		currentDecay := toFloat64(row["decay_factor"])

		var lastAccessed time.Time
		if t, ok := row["last_accessed_at"].(time.Time); ok {
			lastAccessed = t
		}

		hoursSinceAccess := now.Sub(lastAccessed).Hours()
		newDecay := computeDecay(currentDecay, hoursSinceAccess)

		if math.Abs(newDecay-currentDecay) < 0.001 {
			continue
		}

		updateQuery := fmt.Sprintf(`
			MATCH (e:Engram {id: '%s'})
			SET e.decay_factor = %f`,
			escapeCypher(id), newDecay)
		if err := e.store.Execute(updateQuery); err != nil {
			return fmt.Errorf("update decay for %s: %w", id, err)
		}
	}
	return nil
}

func computeDecay(currentDecay, hoursSinceAccess float64) float64 {
	decay := currentDecay * math.Exp(-decayRate*hoursSinceAccess/24.0)
	if decay < 0 {
		return 0
	}
	if decay > 1 {
		return 1
	}
	return decay
}

func (e *Engine) pruneWeak() error {
	rows, err := e.store.QueryRows(fmt.Sprintf(`
		MATCH (e:Engram)
		WHERE e.decay_factor < %f AND e.importance < 0.3
		RETURN e.id AS id`, pruneThreshold))
	if err != nil {
		return fmt.Errorf("query weak engrams: %w", err)
	}

	for _, row := range rows {
		id, ok := row["id"].(string)
		if !ok {
			continue
		}

		deleteEdges := fmt.Sprintf(`
			MATCH (e:Engram {id: '%s'})-[r:EncodedBy]->()
			DELETE r`, escapeCypher(id))
		e.store.Execute(deleteEdges)

		deleteAssocFrom := fmt.Sprintf(`
			MATCH (e:Engram {id: '%s'})-[r:AssociatedWith]->()
			DELETE r`, escapeCypher(id))
		e.store.Execute(deleteAssocFrom)

		deleteAssocTo := fmt.Sprintf(`
			MATCH ()-[r:AssociatedWith]->(e:Engram {id: '%s'})
			DELETE r`, escapeCypher(id))
		e.store.Execute(deleteAssocTo)

		deleteNode := fmt.Sprintf(`
			MATCH (e:Engram {id: '%s'})
			DELETE e`, escapeCypher(id))
		e.store.Execute(deleteNode)
	}
	return nil
}

func (e *Engine) strengthenFrequent() error {
	rows, err := e.store.QueryRows(`
		MATCH (e:Engram)
		WHERE e.access_count > 5 AND e.decay_factor < 0.9
		RETURN e.id AS id, e.decay_factor AS decay_factor, e.access_count AS access_count`)
	if err != nil {
		return fmt.Errorf("query frequent engrams: %w", err)
	}

	for _, row := range rows {
		id, ok := row["id"].(string)
		if !ok {
			continue
		}
		currentDecay := toFloat64(row["decay_factor"])
		accessCount := toInt64(row["access_count"])

		boost := math.Min(float64(accessCount)*0.01, 0.1)
		newDecay := math.Min(currentDecay+boost, 1.0)

		updateQuery := fmt.Sprintf(`
			MATCH (e:Engram {id: '%s'})
			SET e.decay_factor = %f`,
			escapeCypher(id), newDecay)
		e.store.Execute(updateQuery)
	}
	return nil
}

func (e *Engine) cleanOrphanedCues() error {
	e.store.Execute(`
		MATCH (c:Cue)
		WHERE NOT exists { MATCH ()-[:EncodedBy]->(c) }
		DELETE c`)
	return nil
}
