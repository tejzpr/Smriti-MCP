/* SPDX-License-Identifier: MPL-2.0
 * Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package backup

import "context"

type Noop struct{}

func (n *Noop) Init(_ context.Context) error { return nil }
func (n *Noop) Pull(_ context.Context) error { return nil }
func (n *Noop) Push(_ context.Context) error { return nil }
func (n *Noop) Close() error                 { return nil }
