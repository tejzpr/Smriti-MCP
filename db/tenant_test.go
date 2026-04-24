// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTenantUser_LadybugStore(t *testing.T) {
	store, err := OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory: %v", err)
	}
	defer store.Close()

	assert.Equal(t, "", store.TenantUser(), "LadybugStore should return empty TenantUser")
}

func TestTenantStoreWrapper(t *testing.T) {
	store, err := OpenInMemory()
	if err != nil {
		t.Fatalf("open in-memory: %v", err)
	}
	defer store.Close()

	wrapper := &TenantStoreWrapper{Store: store, User: "alice"}
	assert.Equal(t, "alice", wrapper.TenantUser())
	assert.Equal(t, "ladybug", wrapper.DBType()) // inherited from inner store
	assert.Equal(t, ":memory:", wrapper.Path())  // inherited from inner store
}
