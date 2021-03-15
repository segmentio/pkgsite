// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"fmt"
	"testing"

	"golang.org/x/pkgsite/internal/stdlib"
	"golang.org/x/pkgsite/internal/testing/sample"
)

func TestFileSource(t *testing.T) {
	for _, test := range []struct {
		modulePath, version, filePath, want string
	}{
		{
			modulePath: sample.ModulePath,
			version:    sample.VersionString,
			filePath:   "LICENSE.txt",
			want:       fmt.Sprintf("%s@%s/%s", sample.ModulePath, sample.VersionString, "LICENSE.txt"),
		},
		{
			modulePath: stdlib.ModulePath,
			version:    "v1.13.0",
			filePath:   "README.md",
			want:       fmt.Sprintf("go.googlesource.com/go/+/refs/tags/%s/%s", "go1.13", "README.md"),
		},
		{
			modulePath: stdlib.ModulePath,
			version:    "v1.13.invalid",
			filePath:   "README.md",
			want:       fmt.Sprintf("go.googlesource.com/go/+/refs/heads/master/%s", "README.md"),
		},
	} {
		t.Run(fmt.Sprintf("%s@%s/%s", test.modulePath, test.version, test.filePath), func(t *testing.T) {
			if got := fileSource(test.modulePath, test.version, test.filePath); got != test.want {
				t.Errorf("fileSource(%q, %q, %q) = %q; want = %q", test.modulePath, test.version, test.filePath, got, test.want)
			}
		})
	}
}
