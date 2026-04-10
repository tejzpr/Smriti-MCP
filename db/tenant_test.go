/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

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
