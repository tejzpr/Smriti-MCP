// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package memory

import "time"

type Engram struct {
	ID             string    `json:"id"`
	Content        string    `json:"content"`
	Summary        string    `json:"summary"`
	MemoryType     string    `json:"memory_type"`
	Importance     float64   `json:"importance"`
	AccessCount    int64     `json:"access_count"`
	CreatedAt      time.Time `json:"created_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	DecayFactor    float64   `json:"decay_factor"`
	Embedding      []float32 `json:"embedding,omitempty"`
	Source         string    `json:"source"`
	Tags           string    `json:"tags"`
	ClusterID      int64     `json:"cluster_id"`
}

type Cue struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CueType   string    `json:"cue_type"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type Association struct {
	FromID       string  `json:"from_id"`
	ToID         string  `json:"to_id"`
	RelationType string  `json:"relation_type"`
	Strength     float64 `json:"strength"`
}

type SearchResult struct {
	Engram    Engram  `json:"engram"`
	Score     float64 `json:"score"`
	MatchType string  `json:"match_type"`
	HopDepth  int     `json:"hop_depth,omitempty"`
}

type StoreRequest struct {
	Content    string   `json:"content"`
	Source     string   `json:"source,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Importance float64  `json:"importance,omitempty"`
}

type RecallRequest struct {
	Query    string  `json:"query"`
	Mode     string  `json:"mode"`
	Limit    int     `json:"limit,omitempty"`
	MinScore float64 `json:"min_score,omitempty"`
}

type ManageRequest struct {
	Action   string `json:"action"`
	EngramID string `json:"engram_id,omitempty"`
}
