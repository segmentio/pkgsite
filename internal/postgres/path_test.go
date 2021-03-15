// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/pkgsite/internal/database"
	"golang.org/x/pkgsite/internal/testing/sample"
)

func TestGetLatestMajorPathForV1Path(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	checkLatest := func(t *testing.T, db *DB, versions []string, v1path string, version, suffix string) {
		t.Helper()
		gotPath, gotVer, err := db.GetLatestMajorPathForV1Path(ctx, v1path)
		if err != nil {
			t.Fatal(err)
		}
		want := sample.ModulePath
		if suffix != "" {
			want = want + "/" + suffix
		}
		var wantVer int
		if version == "" {
			wantVer = 1
		} else {
			wantVer, err = strconv.Atoi(strings.TrimPrefix(version, "v"))
			if err != nil {
				t.Fatal(err)
			}
		}
		if gotPath != want || gotVer != wantVer {
			t.Errorf("GetLatestMajorPathForV1Path(%q) = %q, %d, want %q, %d", v1path, gotPath, gotVer, want, wantVer)
		}
	}

	for _, test := range []struct {
		name, want string
		versions   []string
	}{
		{
			"want highest major version",
			"v11",
			[]string{"", "v2", "v11"},
		},
		{
			"only v1 version",
			"",
			[]string{""},
		},
		{
			"no v1 version",
			"v4",
			[]string{"v4"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			testDB, release := acquire(t)
			defer release()

			suffix := "a/b/c"

			for _, v := range test.versions {
				modpath := sample.ModulePath
				if v != "" {
					modpath = modpath + "/" + v
				}
				if v == "" {
					v = sample.VersionString
				} else {
					v = v + ".0.0"
				}
				m := sample.Module(modpath, v, suffix)
				MustInsertModule(ctx, t, testDB, m)
			}
			t.Run("module", func(t *testing.T) {
				v1path := sample.ModulePath
				checkLatest(t, testDB, test.versions, v1path, test.want, test.want)
			})
			t.Run("package", func(t *testing.T) {
				want := test.want
				if test.want != "" {
					want += "/"
				}
				v1path := sample.ModulePath + "/" + suffix
				checkLatest(t, testDB, test.versions, v1path, test.want, want+suffix)
			})
		})
	}
}

func TestUpsertPathConcurrently(t *testing.T) {
	// Verify that we get no constraint violations or other errors when
	// the same path is upserted multiple times concurrently.
	t.Parallel()
	testDB, release := acquire(t)
	defer release()
	ctx := context.Background()

	const n = 10
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			errc <- testDB.db.Transact(ctx, sql.LevelRepeatableRead, func(tx *database.DB) error {
				id, err := upsertPath(ctx, tx, "a/path")
				if err != nil {
					return err
				}
				if id == 0 {
					return errors.New("zero id")
				}
				return nil
			})
		}()

	}
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
}

func TestUpsertPaths(t *testing.T) {
	t.Parallel()
	testDB, release := acquire(t)
	defer release()
	ctx := context.Background()

	check := func(paths []string) {
		got, err := upsertPathsInTx(ctx, testDB.db, paths)
		if err != nil {
			t.Fatal(err)
		}
		checkPathMap(t, got, paths)
	}

	check([]string{"a", "b", "c"})
	check([]string{"b", "c", "d", "e"})
}

func checkPathMap(t *testing.T, got map[string]int, paths []string) {
	t.Helper()
	if g, w := len(got), len(paths); g != w {
		t.Errorf("got %d paths, want %d", g, w)
		return
	}
	for _, p := range paths {
		g, ok := got[p]
		if !ok {
			t.Errorf("missing path %q", p)
		} else if g == 0 {
			t.Errorf("path %q has a 0 ID", p)
		}
	}
}

func TestUpsertPathsConcurrently(t *testing.T) {
	// Verify that we get no constraint violations or other errors when
	// the same set of paths is upserted multiple times concurrently.
	t.Parallel()
	testDB, release := acquire(t)
	defer release()
	ctx := context.Background()

	const n = 10
	paths := make([]string, 100)
	for i := 0; i < len(paths); i++ {
		paths[i] = fmt.Sprintf("p%d", i)
	}
	errc := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			start := (10 * i) % len(paths)
			end := start + 50
			if end > len(paths) {
				end = len(paths)
			}
			sub := paths[start:end]
			got, err := upsertPathsInTx(ctx, testDB.db, sub)
			if err == nil {
				checkPathMap(t, got, sub)

			}
			errc <- err
		}()

	}
	for i := 0; i < n; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
}

func upsertPathsInTx(ctx context.Context, db *database.DB, paths []string) (map[string]int, error) {
	var m map[string]int
	err := db.Transact(ctx, sql.LevelRepeatableRead, func(tx *database.DB) error {
		var err error
		m, err = upsertPaths(ctx, tx, paths)
		return err
	})
	if err != nil {
		return nil, err
	}
	return m, nil
}
