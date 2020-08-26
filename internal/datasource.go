// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import (
	"context"

	"golang.org/x/pkgsite/internal/licenses"
)

// DataSource is the interface used by the frontend to interact with module data.
type DataSource interface {
	// See the internal/postgres package for further documentation of these
	// methods, particularly as they pertain to the main postgres implementation.

	// GetDirectory returns information about a directory, which may also be a module and/or package.
	// The module and version must both be known.
	GetDirectory(ctx context.Context, dirPath, modulePath, version string, pathID int, fields ...FieldSet) (_ *Directory, err error)
	// GetDirectoryMeta returns information about a directory.
	GetDirectoryMeta(ctx context.Context, dirPath, modulePath, version string) (_ *DirectoryMeta, err error)
	// GetImports returns a slice of import paths imported by the package
	// specified by path and version.
	GetImports(ctx context.Context, pkgPath, modulePath, version string) ([]string, error)
	// GetLicenses returns licenses at the given path for given modulePath and version.
	GetLicenses(ctx context.Context, fullPath, modulePath, resolvedVersion string) ([]*licenses.License, error)
	// GetModuleInfo returns the ModuleInfo corresponding to modulePath and
	// version.
	GetModuleInfo(ctx context.Context, modulePath, version string) (*ModuleInfo, error)
	// GetPathInfo returns information about a path.
	GetPathInfo(ctx context.Context, path, inModulePath, inVersion string) (outModulePath, outVersion string, isPackage bool, err error)

	// TODO(golang/go#39629): Deprecate these methods.
	//
	// LegacyGetDirectory returns packages whose import path is in a (possibly
	// nested) subdirectory of the given directory path. When multiple
	// package paths satisfy this query, it should prefer the module with
	// the longest path.
	LegacyGetDirectory(ctx context.Context, dirPath, modulePath, version string, fields FieldSet) (_ *LegacyDirectory, err error)
	// LegacyGetModuleInfo returns the LegacyModuleInfo corresponding to modulePath and
	// version.
	LegacyGetModuleInfo(ctx context.Context, modulePath, version string) (*LegacyModuleInfo, error)
	// LegacyGetModuleLicenses returns all top-level Licenses for the given modulePath
	// and version. (i.e., Licenses contained in the module root directory)
	LegacyGetModuleLicenses(ctx context.Context, modulePath, version string) ([]*licenses.License, error)
	// LegacyGetPackage returns the LegacyVersionedPackage corresponding to the given package
	// pkgPath, modulePath, and version. When multiple package paths satisfy this query, it
	// should prefer the module with the longest path.
	LegacyGetPackage(ctx context.Context, pkgPath, modulePath, version string) (*LegacyVersionedPackage, error)
	// LegacyGetPackagesInModule returns LegacyPackages contained in the module version
	// specified by modulePath and version.
	LegacyGetPackagesInModule(ctx context.Context, modulePath, version string) ([]*LegacyPackage, error)
	// LegacyGetPackageLicenses returns all Licenses that apply to pkgPath, within the
	// module version specified by modulePath and version.
	LegacyGetPackageLicenses(ctx context.Context, pkgPath, modulePath, version string) ([]*licenses.License, error)
	// LegacyGetPsuedoVersionsForModule returns ModuleInfo for all known
	// pseudo-versions for the module corresponding to modulePath.
	LegacyGetPsuedoVersionsForModule(ctx context.Context, modulePath string) ([]*ModuleInfo, error)
	// LegacyGetPsuedoVersionsForModule returns ModuleInfo for all known
	// pseudo-versions for any module containing a package with the given import
	// path.
	LegacyGetPsuedoVersionsForPackageSeries(ctx context.Context, pkgPath string) ([]*ModuleInfo, error)
	// LegacyGetTaggedVersionsForModule returns ModuleInfo for all known tagged
	// versions for the module corresponding to modulePath.
	LegacyGetTaggedVersionsForModule(ctx context.Context, modulePath string) ([]*ModuleInfo, error)
	// LegacyGetTaggedVersionsForModule returns ModuleInfo for all known tagged
	// versions for any module containing a package with the given import path.
	LegacyGetTaggedVersionsForPackageSeries(ctx context.Context, pkgPath string) ([]*ModuleInfo, error)
}
