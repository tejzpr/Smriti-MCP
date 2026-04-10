/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package db

// TenantStoreWrapper wraps any Store and overrides TenantUser() to return
// a specific user. This is useful for testing tenant-property isolation
// without requiring a real Neo4j instance.
type TenantStoreWrapper struct {
	Store
	User string
}

func (w *TenantStoreWrapper) TenantUser() string {
	return w.User
}
