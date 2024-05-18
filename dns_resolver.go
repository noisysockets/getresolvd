// SPDX-License-Identifier: MPL-2.0
/*
 * Copyright (C) 2024 The Noisy Sockets Authors.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 * Portions of this file are based on code originally from the Go project,
 *
 * Copyright (c) 2012 The Go Authors. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *   * Redistributions of source code must retain the above copyright
 *     notice, this list of conditions and the following disclaimer.
 *   * Redistributions in binary form must reproduce the above
 *     copyright notice, this list of conditions and the following disclaimer
 *     in the documentation and/or other materials provided with the
 *     distribution.
 *   * Neither the name of Google Inc. nor the names of its
 *     contributors may be used to endorse or promote products derived from
 *     this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package resolver

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/noisysockets/resolver/internal/addrselect"
	"github.com/noisysockets/resolver/internal/util"
)

var (
	_ Resolver = (*dnsResolver)(nil)
)

type DNSResolverConfig struct {
	// Protocol is the protocol used for DNS resolution.
	Protocol Protocol
	// Servers is the list of DNS servers to query.
	Servers []netip.AddrPort
	// Rotate specifies whether to rotate the list of DNS servers for
	// load balancing (eg. round-robin).
	Rotate bool
	// Timeout is the maximum duration to wait for a query to complete
	// (including retries).
	Timeout time.Duration
	// DialContext is used to establish a connection to a DNS server.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// TLSClientConfig is the configuration for the TLS client used for DNS over TLS.
	TLSClientConfig *tls.Config
}

// dnsResolver is a DNS resolver written in pure Go.
type dnsResolver struct {
	protocol        Protocol
	servers         []netip.AddrPort
	rotate          bool
	timeout         time.Duration
	dialContext     func(ctx context.Context, network, address string) (net.Conn, error)
	tlsClientConfig *tls.Config
}

// DNS returns a new DNS resolver.
func DNS(conf *DNSResolverConfig) *dnsResolver {
	if conf == nil {
		conf = &DNSResolverConfig{}
	}

	dialContext := (&net.Dialer{}).DialContext
	if conf.DialContext != nil {
		dialContext = conf.DialContext
	}

	return &dnsResolver{
		protocol:        conf.Protocol,
		servers:         conf.Servers,
		rotate:          conf.Rotate,
		timeout:         conf.Timeout,
		dialContext:     dialContext,
		tlsClientConfig: conf.TLSClientConfig,
	}
}

// LookupHost looks up the given host using the resolver. It returns a slice of
// that host's addresses.
func (r *dnsResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}

	return util.Strings(addrs), nil
}

// LookupNetIP looks up host using the resolver. It returns a slice of that
// host's IP addresses of the type specified by network. The network must be
// one of "ip", "ip4" or "ip6".
func (r *dnsResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
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
		return []netip.Addr{addr}, nil
	}

	// Is it a domain name?
	addrs, err := r.lookupHost(ctx, network, host)
	if err != nil {
		return nil, err
	}

	return addrs, nil
}

func (r *dnsResolver) lookupHost(ctx context.Context, network, host string) ([]netip.Addr, *net.DNSError) {
	dnsErr := &net.DNSError{
		Name: host,
	}

	// If the host is not a valid domain name, return an error.
	if _, ok := dns.IsDomainName(host); !ok {
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err:        ErrNoSuchHost.Error(),
			IsNotFound: true,
		})
	}

	client := &dns.Client{
		Timeout: r.timeout,
	}

	switch r.protocol {
	case ProtocolUDP:
		client.Net = "udp"
	case ProtocolTCP:
		client.Net = "tcp"
	case ProtocolTLS:
		client.Net = "tcp-tls"
		client.TLSConfig = r.tlsClientConfig
	default:
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err: ErrUnsupportedProtocol.Error(),
		})
	}

	// Rotate the nameserver list for load balancing.
	servers := r.servers
	if r.rotate {
		servers = make([]netip.AddrPort, len(r.servers))
		copy(servers, r.servers)
		servers = util.Shuffle(servers)
	}

	var qTypes []uint16
	switch network {
	case "ip":
		qTypes = []uint16{dns.TypeA, dns.TypeAAAA}
	case "ip4":
		qTypes = []uint16{dns.TypeA}
	case "ip6":
		qTypes = []uint16{dns.TypeAAAA}
	default:
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err: ErrUnsupportedNetwork.Error(),
		})
	}

	name := dns.Fqdn(host)

	var firstErr *net.DNSError
	var addrs []netip.Addr
	for _, server := range servers {
		for _, qType := range qTypes {
			reply, err := r.tryOneName(ctx, client, server, name, qType)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}

			// We asked for recursion, so it should have included all the
			// answers we need in this one packet.
			//
			// Further, RFC 1034 section 4.3.1 says that "the recursive
			// response to a query will be... The answer to the query,
			// possibly preface by one or more CNAME RRs that specify
			// aliases encountered on the way to an answer."
			//
			// Therefore, we should be able to assume that we can ignore
			// CNAMEs and that the A and AAAA records we requested are
			// for the canonical name.

			for _, rr := range reply.Answer {
				switch rr := rr.(type) {
				case *dns.A:
					addrs = append(addrs, netip.AddrFrom4([4]byte(rr.A.To4())))
				case *dns.AAAA:
					addrs = append(addrs, netip.AddrFrom16([16]byte(rr.AAAA.To16())))
				}
			}
		}

		if len(addrs) > 0 {
			dial := func(network, address string) (net.Conn, error) {
				return r.dialContext(ctx, network, address)
			}

			addrselect.SortByRFC6724(dial, addrs)

			return addrs, nil
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}

	return nil, extendDNSError(dnsErr, net.DNSError{
		Err:        ErrNoSuchHost.Error(),
		IsNotFound: true,
	})
}

func (r *dnsResolver) tryOneName(ctx context.Context, client *dns.Client,
	server netip.AddrPort, name string, qType uint16) (*dns.Msg, *net.DNSError) {

	dnsErr := &net.DNSError{
		Server: server.String(),
		Name:   name,
	}

	if client.Timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, client.Timeout)
		defer cancel()
	}

	if server.Port() == 0 {
		if client.Net == "udp" || client.Net == "tcp" {
			server = netip.AddrPortFrom(server.Addr(), 53)
		} else if client.Net == "tcp-tls" {
			server = netip.AddrPortFrom(server.Addr(), 853)
		}
	}

	conn, err := r.dialContext(ctx, strings.TrimSuffix(client.Net, "-tls"), server.String())
	if err != nil {
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err:         err.Error(),
			IsTimeout:   errors.Is(err, context.DeadlineExceeded),
			IsTemporary: true,
		})
	}

	if strings.HasSuffix(client.Net, "-tls") {
		tlsConfig := &tls.Config{}
		if client.TLSConfig != nil {
			tlsConfig = client.TLSConfig.Clone()
		}
		tlsConfig.ServerName = server.Addr().String()

		conn = tls.Client(conn, tlsConfig)
		if err := conn.(*tls.Conn).HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			// Handshake errors are not likely to be temporary.
			return nil, extendDNSError(dnsErr, net.DNSError{
				Err:       err.Error(),
				IsTimeout: errors.Is(err, context.DeadlineExceeded),
			})
		}
	}
	defer conn.Close()

	req := new(dns.Msg)
	req.SetQuestion(name, qType)

	reply, _, err := client.ExchangeWithConn(req, &dns.Conn{Conn: conn})
	if err != nil {
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err:         err.Error(),
			IsTimeout:   errors.Is(err, context.DeadlineExceeded),
			IsTemporary: true,
		})
	}

	switch reply.Rcode {
	case dns.RcodeSuccess:
		return reply, nil
	case dns.RcodeNameError:
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err:        ErrNoSuchHost.Error(),
			IsNotFound: true,
		})
	default:
		return nil, extendDNSError(dnsErr, net.DNSError{
			Err: fmt.Errorf("unexpected return code %s: %w",
				dns.RcodeToString[reply.Rcode], ErrServerMisbehaving).Error(),
			// SERVFAIL is not cached.
			IsTemporary: reply.Rcode == dns.RcodeServerFailure,
		})
	}
}