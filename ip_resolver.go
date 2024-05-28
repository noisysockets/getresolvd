// SPDX-License-Identifier: MPL-2.0
/*
 * Copyright (C) 2024 The Noisy Sockets Authors.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 */

package resolver

import (
	"context"
	"net"
	"net/netip"

	"github.com/noisysockets/resolver/util"
)

// ipResolver is a resolver that looks up IP address strings.
type ipResolver struct{}

// IP returns a resolver that looks up IP address strings.
func IP() Resolver {
	return &ipResolver{}
}

func (r *ipResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}

	return util.Strings(addrs), nil
}

func (r *ipResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	// Is it an IP address of the correct family?
	if addr, err := netip.ParseAddr(host); err == nil {
		switch network {
		case "ip":
			// Nothing to do.
		case "ip4":
			if !addr.Unmap().Is4() {
				return nil, &net.DNSError{
					Err:        ErrNoSuchHost.Error(),
					Name:       host,
					IsNotFound: true,
				}
			}
		case "ip6":
			if !addr.Is6() {
				return nil, &net.DNSError{
					Err:        ErrNoSuchHost.Error(),
					Name:       host,
					IsNotFound: true,
				}
			}
		default:
			return nil, &net.DNSError{
				Err:  ErrUnsupportedNetwork.Error(),
				Name: host,
			}
		}
		return []netip.Addr{addr.Unmap()}, nil
	}

	return nil, &net.DNSError{
		Err:        ErrNoSuchHost.Error(),
		Name:       host,
		IsNotFound: true,
	}
}