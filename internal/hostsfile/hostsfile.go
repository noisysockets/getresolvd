// SPDX-License-Identifier: MPL-2.0
/*
 * Copyright (C) 2024 The Noisy Sockets Authors.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/.
 *
 * Portions of this file are based on code originally:
 *
 * Copyright (c) 2014 Kevin Burke
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of
 * this software and associated documentation files (the "Software"), to deal in
 * the Software without restriction, including without limitation the rights to
 * use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
 * the Software, and to permit persons to whom the Software is furnished to do so,
 * subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
 * FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
 * COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
 * IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
 * CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package hostsfile

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/miekg/dns"
)

// Represents a hosts file. Records match a single line in the file.
type Hostsfile struct {
	records []*Record
}

// Records returns an array of all entries in the hostsfile.
func (h *Hostsfile) Records() []*Record {
	return h.records
}

// A single line in the hosts file
type Record struct {
	IpAddress net.IPAddr
	Hostnames []string
	comment   string
	isBlank   bool
}

func (r *Record) Matches(hostname string) bool {
	for _, hn := range r.Hostnames {
		if hn == dns.CanonicalName(hostname) {
			return true
		}
	}
	return false
}

// Decodes the raw text of a hostsfile into a Hostsfile struct. If a line
// contains both an IP address and a comment, the comment will be lost.
//
// Interface example from the image package.
func Decode(rdr io.Reader) (Hostsfile, error) {
	var h Hostsfile
	scanner := bufio.NewScanner(rdr)
	for scanner.Scan() {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		r := new(Record)
		if len(line) == 0 {
			r.isBlank = true
		} else if line[0] == '#' {
			// comment line or blank line: skip it.
			r.comment = line
		} else {
			vals := strings.Fields(line)
			if len(vals) <= 1 {
				return Hostsfile{}, fmt.Errorf("invalid hostsfile entry: %s", line)
			}
			ip, err := net.ResolveIPAddr("ip", vals[0])
			if err != nil {
				return Hostsfile{}, err
			}
			r = &Record{
				IpAddress: *ip,
			}
			for i := 1; i < len(vals); i++ {
				name := vals[i]
				if len(name) > 0 && name[0] == '#' {
					// beginning of a comment. rest of the line is bunk
					break
				}
				if _, ok := dns.IsDomainName(name); ok {
					r.Hostnames = append(r.Hostnames, dns.CanonicalName(name))
				}
			}
		}
		h.records = append(h.records, r)
	}
	if err := scanner.Err(); err != nil {
		return Hostsfile{}, err
	}
	return h, nil
}
