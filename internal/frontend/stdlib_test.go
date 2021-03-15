// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"testing"

	"golang.org/x/pkgsite/internal"
)

func TestParseStdLibURLPath(t *testing.T) {
	testCases := []struct {
		name, url, wantPath, wantVersion string
	}{
		{
			name:        "latest",
			url:         "/cmd/go",
			wantPath:    "cmd/go",
			wantVersion: internal.LatestVersion,
		},
		{
			name:        "package at version",
			url:         "/cmd/go@go1.13",
			wantPath:    "cmd/go",
			wantVersion: "v1.13.0",
		},
		{
			name:        "package at beta version",
			url:         "/cmd/go@go1.13beta1",
			wantPath:    "cmd/go",
			wantVersion: "v1.13.0-beta.1",
		},
		{
			name:        "std",
			url:         "/std@go1.13",
			wantPath:    "std",
			wantVersion: "v1.13.0",
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseStdLibURLPath(test.url)
			if err != nil {
				t.Fatalf("parseStdLibURLPath(%q): %v)", test.url, err)
			}
			if test.wantVersion != got.requestedVersion || test.wantPath != got.fullPath {
				t.Fatalf("parseStdLibURLPath(%q): %q, %q, %v; want = %q, %q",
					test.url, got.fullPath, got.requestedVersion, err, test.wantPath, test.wantVersion)
			}
		})
	}
}
