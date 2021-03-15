// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fetch provides a way to fetch modules from a proxy.
package fetch

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"golang.org/x/mod/modfile"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/dcensus"
	"golang.org/x/pkgsite/internal/derrors"
	"golang.org/x/pkgsite/internal/licenses"
	"golang.org/x/pkgsite/internal/log"
	"golang.org/x/pkgsite/internal/proxy"
	"golang.org/x/pkgsite/internal/source"
	"golang.org/x/pkgsite/internal/stdlib"
)

var (
	ErrModuleContainsNoPackages = errors.New("module contains 0 packages")
	errMalformedZip             = errors.New("module zip is malformed")
)

var (
	fetchLatency = stats.Float64(
		"go-discovery/worker/fetch-latency",
		"Latency of a fetch request.",
		stats.UnitSeconds,
	)
	fetchesShedded = stats.Int64(
		"go-discovery/worker/fetch-shedded",
		"Count of shedded fetches.",
		stats.UnitDimensionless,
	)
	fetchedPackages = stats.Int64(
		"go-discovery/worker/fetch-package-count",
		"Count of successfully fetched packages.",
		stats.UnitDimensionless,
	)

	// FetchLatencyDistribution aggregates frontend fetch request
	// latency by status code. It does not count shedded requests.
	FetchLatencyDistribution = &view.View{
		Name:        "go-discovery/worker/fetch-latency",
		Measure:     fetchLatency,
		Aggregation: ochttp.DefaultLatencyDistribution,
		Description: "Fetch latency by result status.",
		TagKeys:     []tag.Key{dcensus.KeyStatus},
	}
	// FetchResponseCount counts fetch responses by status.
	FetchResponseCount = &view.View{
		Name:        "go-discovery/worker/fetch-count",
		Measure:     fetchLatency,
		Aggregation: view.Count(),
		Description: "Fetch request count by result status",
		TagKeys:     []tag.Key{dcensus.KeyStatus},
	}
	// FetchPackageCount counts how many packages were successfully fetched.
	FetchPackageCount = &view.View{
		Name:        "go-discovery/worker/fetch-package-count",
		Measure:     fetchedPackages,
		Aggregation: view.Count(),
		Description: "Count of packages successfully fetched",
	}
	// SheddedFetchCount counts the number of fetches that were shedded.
	SheddedFetchCount = &view.View{
		Name:        "go-discovery/worker/fetch-shedded",
		Measure:     fetchesShedded,
		Aggregation: view.Count(),
		Description: "Count of shedded fetches",
	}
)

type FetchResult struct {
	ModulePath       string
	RequestedVersion string
	ResolvedVersion  string
	MainVersion      string
	MasterVersion    string
	// HasGoMod says whether the zip contain a go.mod file. If Module (below) is non-nil, then
	// Module.HasGoMod will be the same value. But HasGoMod will be populated even if Module is nil
	// because there were problems with it, as long as we can download and read the zip.
	HasGoMod             bool
	GoModPath            string
	Status               int
	Error                error
	Defer                func() // caller must defer this on all code paths
	Module               *internal.Module
	PackageVersionStates []*internal.PackageVersionState
}

// FetchModule queries the proxy or the Go repo for the requested module
// version, downloads the module zip, and processes the contents to return an
// *internal.Module and related information.
//
// Even if err is non-nil, the result may contain useful information, like the go.mod path.
//
// Callers of FetchModule must
//   defer fr.Defer()
// immediately after the call.
func FetchModule(ctx context.Context, modulePath, requestedVersion string, proxyClient *proxy.Client, sourceClient *source.Client) (fr *FetchResult) {
	start := time.Now()
	defer func() {
		latency := float64(time.Since(start).Seconds())
		dcensus.RecordWithTag(ctx, dcensus.KeyStatus, strconv.Itoa(fr.Status), fetchLatency.M(latency))
		if fr.Status < 300 {
			stats.Record(ctx, fetchedPackages.M(int64(len(fr.PackageVersionStates))))
		}
	}()

	fr = &FetchResult{
		ModulePath:       modulePath,
		RequestedVersion: requestedVersion,
		Defer:            func() {},
	}
	defer derrors.Wrap(&fr.Error, "FetchModule(%q, %q)", modulePath, requestedVersion)

	fi, err := fetchModule(ctx, fr, proxyClient, sourceClient)
	fr.Error = err
	if err != nil {
		fr.Status = derrors.ToStatus(fr.Error)
	}
	if fr.Status == 0 {
		fr.Status = http.StatusOK
	}
	if fi != nil {
		finishFetchInfo(fi, fr.Status, fr.Error)
	}
	return fr
}

func fetchModule(ctx context.Context, fr *FetchResult, proxyClient *proxy.Client, sourceClient *source.Client) (*FetchInfo, error) {
	info, err := GetInfo(ctx, fr.ModulePath, fr.RequestedVersion, proxyClient)
	if err != nil {
		return nil, err
	}
	fr.ResolvedVersion = info.Version
	commitTime := info.Time

	var zipSize int64
	if zipLoadShedder != nil {
		var err error
		zipSize, err = getZipSize(ctx, fr.ModulePath, fr.ResolvedVersion, proxyClient)
		if err != nil {
			return nil, err
		}
		// Load shed or mark module as too large.
		// We treat zip size as a proxy for the total memory consumed by
		// processing a module, and use it to decide whether we can currently
		// afford to process a module.
		shouldShed, deferFunc := zipLoadShedder.shouldShed(uint64(zipSize))
		fr.Defer = deferFunc
		if shouldShed {
			stats.Record(ctx, fetchesShedded.M(1))
			return nil, fmt.Errorf("%w: size=%dMi", derrors.SheddingLoad, zipSize/mib)
		}
		if zipSize > maxModuleZipSize {
			log.Warningf(ctx, "FetchModule: %s@%s zip size %dMi exceeds max %dMi",
				fr.ModulePath, fr.ResolvedVersion, zipSize/mib, maxModuleZipSize/mib)
			return nil, derrors.ModuleTooLarge
		}
	}

	// Proceed with the fetch.
	fi := &FetchInfo{
		ModulePath: fr.ModulePath,
		Version:    fr.ResolvedVersion,
		ZipSize:    uint64(zipSize),
		Start:      time.Now(),
	}
	startFetchInfo(fi)

	var zipReader *zip.Reader
	if fr.ModulePath == stdlib.ModulePath {
		var resolvedVersion string
		zipReader, resolvedVersion, commitTime, err = stdlib.Zip(fr.RequestedVersion)
		if err != nil {
			return fi, err
		}
		// If the requested version is a branch name like "master" or "main", we cannot
		// determine the right resolved version until we start working with the repo.
		fr.ResolvedVersion = resolvedVersion
		fi.Version = resolvedVersion
	} else {
		zipReader, err = proxyClient.Zip(ctx, fr.ModulePath, fr.ResolvedVersion)
		if err != nil {
			return fi, err
		}
	}

	// Set fr.HasGoMod as early as possible, because the go command uses it to
	// decide the latest version in some cases (see fetchRawLatestVersion in
	// this package) and all it requires is a valid zip.
	if fr.ModulePath == stdlib.ModulePath {
		fr.HasGoMod = true
	} else {
		fr.HasGoMod = hasGoModFile(zipReader, fr.ModulePath, fr.ResolvedVersion)
	}

	// getGoModPath may return a non-empty goModPath even if the error is
	// non-nil, if the module version is an alternative module.
	var goModBytes []byte
	fr.GoModPath, goModBytes, err = getGoModPath(ctx, fr.ModulePath, fr.ResolvedVersion, proxyClient)
	if err != nil {
		return fi, err
	}

	mod, pvs, err := processZipFile(ctx, fr.ModulePath, fr.ResolvedVersion, commitTime, zipReader, sourceClient)
	if err != nil {
		return fi, err
	}
	mod.HasGoMod = fr.HasGoMod
	if goModBytes != nil {
		if err := processGoModFile(goModBytes, mod); err != nil {
			return fi, fmt.Errorf("%v: %w", err.Error(), derrors.BadModule)
		}
	}
	fr.Module = mod
	fr.PackageVersionStates = pvs
	for _, state := range fr.PackageVersionStates {
		if state.Status != http.StatusOK {
			fr.Status = derrors.ToStatus(derrors.HasIncompletePackages)
		}
	}
	return fi, nil
}

// GetInfo returns the result of a request to the proxy .info endpoint. If
// the modulePath is "std", a request to @master will return an empty
// commit time.
func GetInfo(ctx context.Context, modulePath, requestedVersion string, proxyClient *proxy.Client) (_ *proxy.VersionInfo, err error) {
	if modulePath == stdlib.ModulePath {
		var resolvedVersion string
		resolvedVersion, err = stdlib.ZipInfo(requestedVersion)
		if err != nil {
			return nil, err
		}
		return &proxy.VersionInfo{Version: resolvedVersion}, nil
	}
	return proxyClient.Info(ctx, modulePath, requestedVersion)
}

func getZipSize(ctx context.Context, modulePath, resolvedVersion string, proxyClient *proxy.Client) (_ int64, err error) {
	if modulePath == stdlib.ModulePath {
		return stdlib.EstimatedZipSize, nil
	}
	return proxyClient.ZipSize(ctx, modulePath, resolvedVersion)
}

// getGoModPath returns the module path from the go.mod file, as well as the contents of the file obtained from the proxy.
// If modulePath is the standardl library, then the contents will be nil.
func getGoModPath(ctx context.Context, modulePath, resolvedVersion string, proxyClient *proxy.Client) (string, []byte, error) {
	if modulePath == stdlib.ModulePath {
		return stdlib.ModulePath, nil, nil
	}
	goModBytes, err := proxyClient.Mod(ctx, modulePath, resolvedVersion)
	if err != nil {
		return "", nil, err
	}
	goModPath := modfile.ModulePath(goModBytes)
	if goModPath == "" {
		return "", nil, fmt.Errorf("go.mod has no module path: %w", derrors.BadModule)
	}
	if goModPath != modulePath {
		// The module path in the go.mod file doesn't match the path of the
		// zip file. Don't insert the module. Store an AlternativeModule
		// status in module_version_states.
		return goModPath, goModBytes, fmt.Errorf("module path=%s, go.mod path=%s: %w", modulePath, goModPath, derrors.AlternativeModule)
	}
	return goModPath, goModBytes, nil
}

// processZipFile extracts information from the module version zip.
func processZipFile(ctx context.Context, modulePath string, resolvedVersion string, commitTime time.Time, zipReader *zip.Reader, sourceClient *source.Client) (_ *internal.Module, _ []*internal.PackageVersionState, err error) {
	defer derrors.Wrap(&err, "processZipFile(%q, %q)", modulePath, resolvedVersion)

	ctx, span := trace.StartSpan(ctx, "fetch.processZipFile")
	defer span.End()

	sourceInfo, err := source.ModuleInfo(ctx, sourceClient, modulePath, resolvedVersion)
	if err != nil {
		log.Infof(ctx, "error getting source info: %v", err)
	}
	readmes, err := extractReadmesFromZip(modulePath, resolvedVersion, zipReader)
	if err != nil {
		return nil, nil, fmt.Errorf("extractReadmesFromZip(%q, %q, zipReader): %v", modulePath, resolvedVersion, err)
	}
	logf := func(format string, args ...interface{}) {
		log.Infof(ctx, format, args...)
	}
	d := licenses.NewDetector(modulePath, resolvedVersion, zipReader, logf)
	allLicenses := d.AllLicenses()
	packages, packageVersionStates, err := extractPackagesFromZip(ctx, modulePath, resolvedVersion, zipReader, d, sourceInfo)
	if errors.Is(err, ErrModuleContainsNoPackages) || errors.Is(err, errMalformedZip) {
		return nil, nil, fmt.Errorf("%v: %w", err.Error(), derrors.BadModule)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("extractPackagesFromZip(%q, %q, zipReader, %v): %v", modulePath, resolvedVersion, allLicenses, err)
	}
	return &internal.Module{
		ModuleInfo: internal.ModuleInfo{
			ModulePath:        modulePath,
			Version:           resolvedVersion,
			CommitTime:        commitTime,
			IsRedistributable: d.ModuleIsRedistributable(),
			SourceInfo:        sourceInfo,
			// HasGoMod is populated by the caller.
		},
		Licenses: allLicenses,
		Units:    moduleUnits(modulePath, resolvedVersion, packages, readmes, d),
	}, packageVersionStates, nil
}

func hasGoModFile(zr *zip.Reader, m, v string) bool {
	return zipFile(zr, path.Join(moduleVersionDir(m, v), "go.mod")) != nil
}

// processGoModFile populates mod with information extracted from the contents of the go.mod file.
func processGoModFile(goModBytes []byte, mod *internal.Module) (err error) {
	defer derrors.Wrap(&err, "processGoModFile")

	mf, err := modfile.Parse("go.mod", goModBytes, nil)
	if err != nil {
		return err
	}
	mod.Deprecated, mod.DeprecationComment = extractDeprecatedComment(mf)
	return nil
}

// extractDeprecatedComment looks for "Deprecated" comments in the line comments
// before the module declaration. If it finds one, it returns true along with
// the text after "Deprecated:". Otherwise it returns false, "".
func extractDeprecatedComment(mf *modfile.File) (bool, string) {
	const prefix = "Deprecated:"

	if mf.Module == nil {
		return false, ""
	}
	for _, comment := range append(mf.Module.Syntax.Before, mf.Module.Syntax.Suffix...) {
		text := strings.TrimSpace(strings.TrimPrefix(comment.Token, "//"))
		if strings.HasPrefix(text, prefix) {
			return true, strings.TrimSpace(text[len(prefix):])
		}
	}
	return false, ""
}

// moduleVersionDir formats the content subdirectory for the given
// modulePath and version.
func moduleVersionDir(modulePath, version string) string {
	return fmt.Sprintf("%s@%s", modulePath, version)
}

// zipFile returns the file in r whose name matches the given name, or nil
// if there isn't one.
func zipFile(r *zip.Reader, name string) *zip.File {
	for _, f := range r.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

type FetchInfo struct {
	ModulePath string
	Version    string
	ZipSize    uint64
	Start      time.Time
	Finish     time.Time
	Status     int
	Error      error
}

var (
	fetchInfoMu  sync.Mutex
	fetchInfoMap = map[*FetchInfo]struct{}{}
)

func init() {
	const linger = time.Minute
	go func() {
		for {
			now := time.Now()
			fetchInfoMu.Lock()
			for fi := range fetchInfoMap {
				if !fi.Finish.IsZero() && now.Sub(fi.Finish) > linger {
					delete(fetchInfoMap, fi)
				}
			}
			fetchInfoMu.Unlock()
			time.Sleep(linger)
		}
	}()
}

func startFetchInfo(fi *FetchInfo) {
	fetchInfoMu.Lock()
	defer fetchInfoMu.Unlock()
	fetchInfoMap[fi] = struct{}{}
}

func finishFetchInfo(fi *FetchInfo, status int, err error) {
	fetchInfoMu.Lock()
	defer fetchInfoMu.Unlock()
	fi.Finish = time.Now()
	fi.Status = status
	fi.Error = err
}

// FetchInfos returns information about all fetches in progress,
// sorted by start time.
func FetchInfos() []*FetchInfo {
	var fis []*FetchInfo
	fetchInfoMu.Lock()
	for fi := range fetchInfoMap {
		// Copy to avoid races on Status and Error when read by
		// worker home page.
		cfi := *fi
		fis = append(fis, &cfi)
	}
	fetchInfoMu.Unlock()
	// Order first by done-ness, then by age.
	sort.Slice(fis, func(i, j int) bool {
		if (fis[i].Status == 0) == (fis[j].Status == 0) {
			return fis[i].Start.Before(fis[j].Start)
		}
		return fis[i].Status == 0
	})
	return fis
}
