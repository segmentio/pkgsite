// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fetch

import (
	"path"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/stdlib"
	"golang.org/x/pkgsite/internal/testing/sample"
)

func TestDirectoryPaths(t *testing.T) {
	for _, test := range []struct {
		name, modulePath string
		packageSuffixes  []string
		want             []string
	}{
		{
			name:       "no packages",
			modulePath: "github.com/empty/module",
			want:       []string{"github.com/empty/module"},
		},
		{
			name:            "only root package",
			modulePath:      "github.com/russross/blackfriday",
			packageSuffixes: []string{""},
			want:            []string{"github.com/russross/blackfriday"},
		},
		{
			name:       "multiple packages and directories",
			modulePath: "github.com/elastic/go-elasticsearch/v7",
			packageSuffixes: []string{
				"esapi",
				"estransport",
				"esutil",
				"internal/version",
			},
			want: []string{
				"github.com/elastic/go-elasticsearch/v7",
				"github.com/elastic/go-elasticsearch/v7/esapi",
				"github.com/elastic/go-elasticsearch/v7/estransport",
				"github.com/elastic/go-elasticsearch/v7/esutil",
				"github.com/elastic/go-elasticsearch/v7/internal",
				"github.com/elastic/go-elasticsearch/v7/internal/version",
			},
		},
		{
			name:            "std lib",
			modulePath:      stdlib.ModulePath,
			packageSuffixes: []string{"cmd/go"},
			want:            []string{"cmd", "cmd/go", "std"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var packages []*goPackage
			for _, suffix := range test.packageSuffixes {
				packages = append(packages, samplePackage(test.modulePath, suffix))
			}
			got := unitPaths(test.modulePath, packages)
			sort.Strings(got)
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("unitPaths(%q, %q)  mismatch (-want +got):\n%s",
					test.modulePath, test.packageSuffixes, diff)
			}
		})
	}
}

// samplePackage constructs a package with the given module path and suffix.
//
// If modulePath is the standard library, the package path is the
// suffix, which must not be empty. Otherwise, the package path
// is the concatenation of modulePath and suffix.
//
// The package name is last component of the package path.
func samplePackage(modulePath, suffix string) *goPackage {
	p := constructFullPath(modulePath, suffix)
	return &goPackage{
		name:              path.Base(p),
		path:              p,
		v1path:            internal.V1Path(p, modulePath),
		isRedistributable: true,
		licenseMeta:       sample.LicenseMetadata(),
		imports:           sample.Imports(),
		docs: []*internal.Documentation{{
			GOOS:     sample.GOOS,
			GOARCH:   sample.GOARCH,
			Synopsis: sample.Doc.Synopsis,
		}},
	}
}

func constructFullPath(modulePath, suffix string) string {
	if modulePath != stdlib.ModulePath {
		return path.Join(modulePath, suffix)
	}
	return suffix
}
