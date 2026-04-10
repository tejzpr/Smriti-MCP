/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package memory

import (
	"fmt"
	"log"
	"math"

	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
)

const (
	leidenMinNodes     = 3
	leidenRetunePct    = 0.10
	leidenProfileLow   = 0.1
	leidenProfileHigh  = 10.0
	leidenProfileGrain = 1.0
	leidenEffort       = 5
)

func (e *Engine) runLeiden() error {
	// 1. Query all Engram IDs
	var nodeRows []map[string]any
	var err error
	q := `MATCH (e:Engram)` + tenantFilter(e.store, "e") + ` RETURN e.id AS id`
	if isTenant(e.store) {
		nodeRows, err = e.store.PreparedQueryRows(q, tenantParam(e.store, nil))
	} else {
		nodeRows, err = e.store.QueryRows(q)
	}
	if err != nil {
		return fmt.Errorf("query engram ids: %w", err)
	}
	if len(nodeRows) < leidenMinNodes {
		return nil
	}

	// Build bidirectional maps: stringID <-> int64 nodeID
	idToNode := make(map[string]int64, len(nodeRows))
	nodeToID := make(map[int64]string, len(nodeRows))
	for i, row := range nodeRows {
		id, ok := row["id"].(string)
		if !ok {
			continue
		}
		nid := int64(i)
		idToNode[id] = nid
		nodeToID[nid] = id
	}

	// 2. Query all AssociatedWith edges
	eq := `MATCH (e1:Engram)-[r:AssociatedWith]->(e2:Engram)` + tenantFilter(e.store, "e1") + `
		RETURN e1.id AS from_id, e2.id AS to_id, r.strength AS strength`
	var edgeRows []map[string]any
	if isTenant(e.store) {
		edgeRows, err = e.store.PreparedQueryRows(eq, tenantParam(e.store, nil))
	} else {
		edgeRows, err = e.store.QueryRows(eq)
	}
	if err != nil {
		return fmt.Errorf("query associations: %w", err)
	}
	if len(edgeRows) == 0 {
		return nil
	}

	// 3. Build weighted undirected graph
	g := simple.NewWeightedUndirectedGraph(0, 0)
	for nid := range nodeToID {
		g.AddNode(simple.Node(nid))
	}
	for _, row := range edgeRows {
		fromStr, ok1 := row["from_id"].(string)
		toStr, ok2 := row["to_id"].(string)
		if !ok1 || !ok2 {
			continue
		}
		fromNid, ok3 := idToNode[fromStr]
		toNid, ok4 := idToNode[toStr]
		if !ok3 || !ok4 {
			continue
		}
		if fromNid == toNid {
			continue
		}
		strength := toFloat64(row["strength"])
		if strength <= 0 {
			strength = 0.01
		}
		existing := g.WeightedEdgeBetween(fromNid, toNid)
		if existing != nil {
			// Keep the stronger edge
			if existing.Weight() >= strength {
				continue
			}
			g.RemoveEdge(fromNid, toNid)
		}
		g.SetWeightedEdge(simple.WeightedEdge{
			F: simple.Node(fromNid),
			T: simple.Node(toNid),
			W: strength,
		})
	}

	// 4. Determine resolution: smart cache or auto-tune
	currentNodeCount := int64(len(nodeRows))
	resolution := e.leiden.cachedResolution

	if resolution <= 0 || needsRetune(e.leiden.cachedNodeCount, currentNodeCount) {
		resolution = e.autoTuneResolution(g)
		e.leiden.cachedResolution = resolution
		e.leiden.cachedNodeCount = currentNodeCount
		log.Printf("leiden: auto-tuned resolution=%.2f for %d nodes", resolution, currentNodeCount)
	}

	// 5. Run Leiden
	reduced := community.Leiden(g, resolution, nil)
	communities := reduced.Communities()

	// 6. Build cluster assignments
	clusterMap := make(map[string]int64, len(idToNode))
	for clusterID, comm := range communities {
		for _, node := range comm {
			if strID, ok := nodeToID[node.ID()]; ok {
				clusterMap[strID] = int64(clusterID)
			}
		}
	}

	// 7. Write cluster_ids back to DB
	for engramID, clusterID := range clusterMap {
		if err := e.store.PreparedExecute(
			`MATCH (e:Engram {id: $eid}) SET e.cluster_id = $cid`,
			tenantParam(e.store, map[string]any{"eid": engramID, "cid": clusterID}),
		); err != nil {
			return fmt.Errorf("set cluster_id for %s: %w", engramID, err)
		}
	}

	log.Printf("leiden: assigned %d nodes to %d clusters", len(clusterMap), len(communities))
	return nil
}

func (e *Engine) autoTuneResolution(g *simple.WeightedUndirectedGraph) float64 {
	scoreFn := community.LeidenScore(g, community.Weight, leidenEffort, nil)

	profile, err := community.Profile(scoreFn, false, leidenProfileGrain, leidenProfileLow, leidenProfileHigh)
	if err != nil {
		log.Printf("leiden: profile error (using default resolution=1.0): %v", err)
		return 1.0
	}
	if len(profile) == 0 {
		return 1.0
	}

	// Pick the interval with the best score
	bestIdx := 0
	bestScore := math.Inf(-1)
	for i, interval := range profile {
		if interval.Score > bestScore {
			bestScore = interval.Score
			bestIdx = i
		}
	}

	// Use the midpoint of the best interval
	best := profile[bestIdx]
	resolution := (best.Low + best.High) / 2
	if resolution <= 0 {
		resolution = 1.0
	}
	return resolution
}

func needsRetune(cachedCount, currentCount int64) bool {
	if cachedCount <= 0 {
		return true
	}
	growth := float64(currentCount-cachedCount) / float64(cachedCount)
	return math.Abs(growth) >= leidenRetunePct
}
