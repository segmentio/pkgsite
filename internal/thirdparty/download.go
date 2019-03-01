// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The command download copies source code from a package in
// https://go.googlesource.com/go/+/refs/heads/master/src/cmd/go/internal/ and
// writes it to internal/thirdparty. Import paths for cmd/go/internal are
// placed with internal/thirdparty.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	gitRemoteURL = "https://go.googlesource.com/go"

	newImportPath = "golang.org/x/discovery/internal/thirdparty/"
	oldImportPath = "cmd/go/internal/"
)

// readLines reads the contents of filename and returns each line of the file.
func readLines(filename string) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var contents []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		contents = append(contents, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner.Err: %v", err)
	}
	return contents, nil
}

// validatePackage checks if pkg is a valid package in cmd/go/internal.
func validatePackage(pkg string) error {
	// Check specified package exists.
	originURL := fmt.Sprintf("%s/+/refs/heads/master/src/cmd/go/internal/%s", gitRemoteURL, pkg)
	resp, err := http.Head(originURL)
	if err != nil {
		return fmt.Errorf("http.Get(%q): %v", originURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("%q is not a valid cmd/go/internal package.", pkg)
		return fmt.Errorf("http.Get(%q) returned %d (%q)", originURL, resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return nil
}

// commitForMaster returns the commit hash of the current master at origin.
func commitForMaster() string {
	cmd := exec.Command("git", "ls-remote", gitRemoteURL, "rev-parse", "HEAD")
	log.Println(strings.Join(cmd.Args, " "))
	out, err := cmd.Output()
	log.Println(string(out))
	if err != nil {
		log.Fatalf("cmd.Run(%q): %v", strings.Join(cmd.Args, " "), err)
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		log.Fatalf("Unexpected output: %q", string(out))
	}
	return parts[0][0:8]
}

// copyPackage makes a copy of the package at
// https://go.googlesource.com/go/+/refs/heads/master/src/cmd/go/internal/<package>
// into thirdpartyDir.
func copyPackage(pkg, thirdpartyDir string) {
	tempDir, err := ioutil.TempDir("", "go_")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cmd := exec.Command("git", "clone", gitRemoteURL, tempDir)
	log.Println(strings.Join(cmd.Args, " "))
	if err = cmd.Run(); err != nil {
		log.Fatalf("cmd.Run(%q): %v", strings.Join(cmd.Args, " "), err)
	}

	// Copy specified module into internal/thirdparty/.
	path := "src/cmd/go/internal"
	cmd = exec.Command("cp", "-r", fmt.Sprintf("%s/%s/%s", tempDir, path, pkg), thirdpartyDir)
	log.Println(strings.Join(cmd.Args, " "))
	if err = cmd.Run(); err != nil {
		log.Fatalf("cmd.Run(%q): %v", strings.Join(cmd.Args, " "), err)
	}
}

// prependAndReplaceImports prepends the file with a message to warn users not
// make any edits to the file. It also finds occurrences of "cmd/go/internal/"
// and replaces them with "golang.org/x/discovery/internal/thirdparty/".
func prependAndReplaceImports(filename, commit, pkg string) error {
	log.Printf("Editing: %s", filename)

	// Create a temporary file for writing. This will be renamed at the end of the function.
	wf, err := os.OpenFile(fmt.Sprintf("%s_tmp", filename), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer wf.Close()

	writer := bufio.NewWriter(wf)
	fmt.Fprintln(writer, "// DO NOT EDIT. This file was copied from")
	fmt.Fprintln(writer, fmt.Sprintf("// %s/+/%s/src/cmd/go/internal/%s.", gitRemoteURL, commit, pkg))
	fmt.Fprintln(writer, fmt.Sprintf("// generated by internal/thirdparty/download.go -pkg=%s", pkg))
	fmt.Fprintln(writer, "")

	contents, err := readLines(filename)
	if err != nil {
		return err
	}

	pkgToDownload := map[string]bool{}
	for _, line := range contents {
		if strings.Contains(line, oldImportPath) {
			parts := strings.Split(line, oldImportPath)
			line = strings.Join(parts, newImportPath)
			pkgToDownload[newImportPath] = true
		}
		fmt.Fprintln(writer, fmt.Sprintf("%s", line))
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	return os.Rename(fmt.Sprintf("%s_tmp", filename), filename)
}

// editFiles applies prependAndReplaceImports on each file inside pkgDir.
func editFiles(pkg, pkgDir string) {
	commit := commitForMaster()
	if err := filepath.Walk(pkgDir, func(path string, info os.FileInfo, err error) error {
		fileInfo, err := os.Stat(path)
		if err != nil {
			log.Fatalf("os.Stat(%q): %v", path, err)
		}
		if !fileInfo.IsDir() && filepath.Ext(path) == ".go" {
			if err := prependAndReplaceImports(path, commit, pkg); err != nil {
				log.Fatalf("prepend(%q): %v", path, err)
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("filepath.Walk: %v", err)
	}
}

func main() {
	var (
		pkg    = flag.String("pkg", "", "name of package to download inside cmd/go/internal/")
		update = flag.Bool("update", false, "update existing package to latest version of master at origin")
	)

	flag.Parse()

	if *pkg == "" {
		log.Printf(`
Please specify a package inside %s using the -pkg flag.

For example, to download cmd/go/internal/semver: go run download.go -pkg=semver`, fmt.Sprintf("%s/+/refs/heads/master/src/cmd/go/internal/", gitRemoteURL))
		os.Exit(1)
	}
	if err := validatePackage(*pkg); err != nil {
		log.Fatalf("validatePackage(%q): %v", *pkg, err)
	}

	// Get the abs path for internal/thirdparty.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatal("No caller information")
	}
	thirdpartyDir := path.Dir(filename)

	// Validate use of the -update flag.
	pkgDir := fmt.Sprintf("%s/%s", thirdpartyDir, *pkg)
	if *update {
		if _, err := os.Stat(pkgDir); err == nil {
			os.RemoveAll(pkgDir)
		} else {
			log.Fatalf("Update failed for %q: %v", *pkg, err)
		}
	} else {
		if _, err := os.Stat(pkgDir); err == nil {
			log.Fatalf("Download failed: %q already exists. Specify -update flag to update to the latest version of master", pkgDir)
		} else if !os.IsNotExist(err) {
			log.Fatalf("Download failed for %q: %v", *pkg, err)
		}
	}

	copyPackage(*pkg, thirdpartyDir)
	editFiles(*pkg, pkgDir)

	log.Println(fmt.Sprintf("Done! Run tests inside %q to make sure it builds properly.", *pkg))
}
