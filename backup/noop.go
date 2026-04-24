// SPDX-License-Identifier: AGPL-3.0-only
// Copyright 2026 Tejus Pratap <tejzpr@gmail.com>
//
// See LICENSE file for details.

package backup

import "context"

type Noop struct{}

func (n *Noop) Init(_ context.Context) error { return nil }
func (n *Noop) Pull(_ context.Context) error { return nil }
func (n *Noop) Push(_ context.Context) error { return nil }
func (n *Noop) Close() error                 { return nil }
