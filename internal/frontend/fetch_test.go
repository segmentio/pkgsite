// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package frontend

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/derrors"
	"golang.org/x/pkgsite/internal/postgres"
	"golang.org/x/pkgsite/internal/proxy"
	"golang.org/x/pkgsite/internal/testing/sample"
	"golang.org/x/pkgsite/internal/testing/testhelper"
)

var (
	sourceTimeout       = 1 * time.Second
	testModulePath      = "github.com/module"
	testSemver          = "v1.5.2"
	testFetchTimeout    = 300 * time.Second
	testModulesForProxy = []*proxy.Module{
		{
			ModulePath: testModulePath,
			Version:    testSemver,
			Files: map[string]string{
				"bar/foo/foo.go": "// Package foo\npackage foo\n\nconst Foo = 42",
				"README.md":      "This is a readme",
				"LICENSE":        testhelper.MITLicense,
			},
		},
	}
)

func TestFetch(t *testing.T) {
	for _, test := range []struct {
		name, fullPath, version, want string
	}{
		{
			name:     "path at master package is in module root",
			fullPath: testModulePath,
			version:  "master",
		},
		{
			name:     "path at main package is in module root",
			fullPath: testModulePath,
			version:  "main",
		},
		{
			name:     "path at latest package is in module root",
			fullPath: testModulePath,
			version:  internal.LatestVersion,
		},
		{
			name:     "path at semver package is in module root",
			fullPath: testModulePath,
			version:  "v1.5.2",
		},
		{
			name:     "package at latest package is not in module root",
			fullPath: testModulePath + "/bar/foo",
			version:  internal.LatestVersion,
		},
		{
			name:     "directory at master package is not in module root",
			fullPath: testModulePath + "/bar",
			version:  "master",
		},
		{
			name:     "directory at main package is not in module root",
			fullPath: testModulePath + "/bar",
			version:  "main",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _, teardown := newTestServer(t, testModulesForProxy, nil)
			defer teardown()

			ctx, cancel := context.WithTimeout(context.Background(), testFetchTimeout)
			defer cancel()

			status, responseText := s.fetchAndPoll(ctx, s.getDataSource(ctx), testModulePath, test.fullPath, test.version)
			if status != http.StatusOK {
				t.Fatalf("fetchAndPoll(%q, %q, %q) = %d, %s; want status = %d",
					testModulePath, test.fullPath, test.version, status, responseText, http.StatusOK)
			}
		})
	}
}

func TestFetchErrors(t *testing.T) {
	for _, test := range []struct {
		name, modulePath, fullPath, version, wantErrorMessage string
		fetchTimeout                                          time.Duration
		want                                                  int
	}{
		{
			name:             "non-existent module",
			modulePath:       "github.com/nonexistent",
			fullPath:         "github.com/nonexistent",
			version:          internal.LatestVersion,
			want:             http.StatusNotFound,
			wantErrorMessage: "\"github.com/nonexistent\" could not be found.",
		},
		{
			name:             "version invalid",
			modulePath:       testModulePath,
			fullPath:         testModulePath,
			version:          "random-version",
			want:             http.StatusBadRequest,
			wantErrorMessage: "Bad Request",
		},
		{
			name:       "module found but package does not exist",
			modulePath: testModulePath,
			fullPath:   "github.com/module/pkg-nonexistent",
			version:    internal.LatestVersion,
			want:       http.StatusNotFound,
			wantErrorMessage: `
		    &lt;div class=&#34;Error-message&#34;&gt;github.com/module/pkg-nonexistent could not be found.&lt;/div&gt;
		    &lt;div class=&#34;Error-message&#34;&gt;However, you can view &lt;a href=&#34;https://pkg.go.dev/github.com/module&#34;&gt;module github.com/module&lt;/a&gt;.&lt;/div&gt;`,
		},
		{
			name:             "module exists but fetch timed out",
			modulePath:       testModulePath,
			fullPath:         testModulePath,
			version:          internal.LatestVersion,
			fetchTimeout:     1 * time.Millisecond,
			want:             http.StatusRequestTimeout,
			wantErrorMessage: "We're still working on “github.com/module”. Check back in a few minutes!",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.fetchTimeout == 0 {
				test.fetchTimeout = testFetchTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), test.fetchTimeout)
			defer cancel()

			s, _, teardown := newTestServer(t, testModulesForProxy, nil)
			defer teardown()
			got, err := s.fetchAndPoll(ctx, s.getDataSource(ctx), test.modulePath, test.fullPath, test.version)

			if got != test.want {
				t.Fatalf("fetchAndPoll(ctx, testDB, q, %q, %q, %q): %d; want = %d",
					test.modulePath, test.fullPath, test.version, got, test.want)
			}
			if err != test.wantErrorMessage {
				t.Fatalf("fetchAndPoll(ctx, testDB, q, %q, %q, %q): %d;\ngot = \n%q,\nwantErrorMessage = \n%q",
					test.modulePath, test.fullPath, test.version, got, err, test.wantErrorMessage)
			}
		})
	}
}

func TestFetchPathAlreadyExists(t *testing.T) {
	for _, test := range []struct {
		status, want int
	}{
		{http.StatusOK, http.StatusOK},
		{http.StatusNotFound, http.StatusNotFound},
		{derrors.ToStatus(derrors.AlternativeModule), http.StatusNotFound},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), testFetchTimeout)
			defer cancel()
			postgres.MustInsertModule(ctx, t, testDB, sample.DefaultModule())
			if err := testDB.UpsertVersionMap(ctx, &internal.VersionMap{
				ModulePath:       sample.ModulePath,
				RequestedVersion: sample.VersionString,
				ResolvedVersion:  sample.VersionString,
				Status:           test.status,
			}); err != nil {
				t.Fatal(err)
			}

			s, _, teardown := newTestServer(t, testModulesForProxy, nil)
			defer teardown()
			got, _ := s.fetchAndPoll(ctx, s.getDataSource(ctx), sample.ModulePath, sample.PackagePath, sample.VersionString)
			if got != test.want {
				t.Fatalf("fetchAndPoll for status %d: %d; want = %d)", test.status, got, test.want)
			}
		})
	}
}

func TestCandidateModulePaths(t *testing.T) {
	maxPathsToFetch = 7
	for _, test := range []struct {
		name, fullPath string
		want           []string
	}{
		{"custom path", "my.module/foo", []string{
			"my.module/foo",
			"my.module",
		}},
		{"github.com module path", sample.ModulePath, []string{sample.ModulePath}},
		{"more than 7 possible paths", sample.ModulePath + "/1/2/3/4/5/6/7/8/9", []string{
			sample.ModulePath + "/1/2/3/4/5/6",
			sample.ModulePath + "/1/2/3/4/5",
			sample.ModulePath + "/1/2/3/4",
			sample.ModulePath + "/1/2/3",
			sample.ModulePath + "/1/2",
			sample.ModulePath + "/1",
			sample.ModulePath,
		}},
	} {
		t.Run(test.fullPath, func(t *testing.T) {
			got, err := candidateModulePaths(test.fullPath)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Fatalf("mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
