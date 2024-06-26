// SPDX-License-Identifier: MPL-2.0
/*
 * Copyright (C) 2024 The Noisy Sockets Authors.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 */

package testutil

import (
	"context"
	"net/netip"

	"github.com/stretchr/testify/mock"
)

// MockResolver is a mock implementation of Resolver.
type MockResolver struct {
	mock.Mock
}

func (m *MockResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	args := m.Called(ctx, network, host)
	return args.Get(0).([]netip.Addr), args.Error(1)
}
