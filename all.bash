#!/usr/bin/env bash
# Copyright 2019 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

source devtools/lib.sh || { echo "Are you at repo root?"; exit 1; }

# Support ** in globs, for check_script_hashes.
shopt -s globstar

warnout() {
  while read line; do
    warn "$line"
  done
}

# filter FILES GLOB1 GLOB2 ...
# returns the files in FILES that match any of the glob patterns.
filter() {
  local infiles=$1
  shift
  local patterns=$*
  local outfiles=

  for pat in $patterns; do
    for f in $infiles; do
      if [[ $f == $pat ]]; then
        outfiles="$outfiles $f"
      fi
    done
  done
  echo $outfiles
}

# Return the files that are modified or added.
# If there are such files in the working directory, whether or not
# they are staged for commit, use those.
# Otherwise, use the files changed since the previous commit.
modified_files() {
  local working="$(diff_files '') $(diff_files --cached)"
  if [[ $working != ' ' ]]; then
    echo $working
  elif [[ $(git rev-parse HEAD) = $(git rev-parse master) ]]; then
    echo ""
  else
    diff_files HEAD^
  fi
}


# Helper for modified_files. It asks git for all modified, added or deleted
# files, and keeps only the latter two.
diff_files() {
  git diff --name-status $* | awk '$1 ~ /^R/ { print $3; next } $1 != "D" { print $2 }'
}

# codedirs lists directories that contain discovery code. If they include
# directories containing external code, those directories must be excluded in
# findcode below.
codedirs=(
  "cmd"
  "content"
  "internal"
  "migrations"
)

# verify_header checks that all given files contain the standard header for Go
# projects.
verify_header() {
  if [[ "$@" != "" ]]; then
    for FILE in $@
    do
        # Allow for the copyright header to start on either of the first three
        # lines, to accommodate conventions for CSS and HTML.
        line="$(head -4 $FILE)"
        if [[ ! $line == *"The Go Authors. All rights reserved."* ]] &&
         [[ ! $line == "// DO NOT EDIT. This file was copied from" ]]; then
              err "missing license header: $FILE"
        fi
    done
  fi
}

# findcode finds source files in the repo, skipping third-party source.
findcode() {
  find ${codedirs[@]} \
    -not -path '*/third_party/*' \
    \( -name *.go -o -name *.sql -o -name *.tmpl -o -name *.css -o -name *.js \)
}

# ensure_go_binary verifies that a binary exists in $PATH corresponding to the
# given go-gettable URI. If no such binary exists, it is fetched via `go get`.
ensure_go_binary() {
  local binary=$(basename $1)
  if ! [ -x "$(command -v $binary)" ]; then
    info "Installing: $1"
    # Run in a subshell for convenience, so that we don't have to worry about
    # our PWD.
    (set -x; cd && env GO111MODULE=on go get -u $1)
  fi
}

# check_headers checks that all source files that have been staged in this
# commit, and all other non-third-party files in the repo, have a license
# header.
check_headers() {
  if [[ $# -gt 0 ]]; then
    info "Checking listed files for license header"
    verify_header $*
  else
    info "Checking staged files for license header"
    # Check code files that have been modified or added.
    verify_header $(git diff --cached --name-status | grep -vE "^D" | cut -f 2- | grep -E ".go$|.sql$|.sh$")
    info "Checking internal files for license header"
    verify_header $(findcode)
  fi
}

# bad_migrations outputs migrations with bad sequence numbers.
bad_migrations() {
  ls migrations | cut -d _ -f 1 | sort | uniq -c | grep -vE '^\s+2 '
}

# check_bad_migrations looks for sql migration files with bad sequence numbers,
# possibly resulting from a bad merge.
check_bad_migrations() {
  info "Checking for bad migrations"
  bad_migrations | while read line
  do
    err "unexpected number of migrations: $line"
  done
}

# check_unparam runs unparam on source files.
check_unparam() {
  ensure_go_binary mvdan.cc/unparam
  runcmd unparam ./...
}

# check_vet runs go vet on source files.
check_vet() {
  runcmd go vet -all ./...
}

# check_staticcheck runs staticcheck on source files.
check_staticcheck() {
  ensure_go_binary honnef.co/go/tools/cmd/staticcheck
  runcmd staticcheck $(go list ./... | grep -v third_party | grep -v internal/doc | grep -v internal/render)
}

# check_misspell runs misspell on source files.
check_misspell() {
  ensure_go_binary github.com/client9/misspell/cmd/misspell
  runcmd misspell cmd/**/*.{go,sh} internal/**/* README.md
}

# check_templates runs go-template-lint on template files. Unfortunately it
# doesn't handler the /helpers/ fileglob correctly, so it is too noisy to be
# included in standard checks.
check_templates() {
  ensure_go_binary sourcegraph.com/sourcegraph/go-template-lint
  runcmd go-template-lint \
    -f=internal/frontend/server.go \
    -t=internal/frontend/server.go \
    -td=content/static/html/pages | warnout
}


script_hash_glob='content/static/html/**/*.tmpl'

# check_script_hashes checks that our CSP hashes match the ones
# for our HTML scripts.
check_script_hashes() {
  runcmd go run ./devtools/cmd/csphash $script_hash_glob
}

# check_script_output checks that the minifed JavaScript output
# matches the TypeScript source files.
check_script_output() {
  runcmd go run ./devtools/cmd/static -check
}

npm() {
  # Run npm install if node_modules directory does not exist.
  if [ ! -d "node_modules" ]; then
    ./devtools/docker_compose.sh nodejs npm install --quiet
  fi
  ./devtools/docker_compose.sh nodejs npm $@
}

run_e2e() {
  # Run npm install if node_modules directory does not exist.
  if [ ! -d "node_modules" ]; then
    ./devtools/docker_compose.sh nodejs npm install --quiet
  fi
  ./devtools/docker_compose.sh --build ci npm run test:jest:e2e -- $@
}

prettier_file_globs='content/static/**/*.{js,css} **/*.md'

# run_prettier runs prettier on CSS, JS, and MD files. Uses globally
# installed prettier if available or a dockerized installation as a
# fallback.
run_prettier() {
  local files=$*
  if [[ $files = '' ]]; then
    files=$prettier_file_globs
  fi
  if [[ -x "$(command -v prettier)" ]]; then
    runcmd prettier --write $files
  elif [[ -x "$(command -v docker-compose)" && "$(docker images -q pkgsite_nodejs)" ]]; then
    runcmd docker-compose -f devtools/config/docker-compose.yaml run --entrypoint=npx \
    nodejs prettier --write $files
  else
    err "prettier must be installed: see https://prettier.io/docs/en/install.html"
  fi
}

go_linters() {
  check_vet
  check_staticcheck
  check_misspell
  check_unparam
}

standard_linters() {
  check_headers
  check_bad_migrations
  go_linters
  check_script_hashes
  check_script_output
}


usage() {
  cat <<EOUSAGE
Usage: $0 [subcommand]
Available subcommands:
  help           - display this help message
  (empty)        - run all standard checks and tests
  ci             - run checks and tests suitable for continuous integration
  cl             - run checks and tests on the current CL, suitable for a commit or pre-push hook
  e2e            - run e2e tests locally.
  lint           - run all standard linters below:
  headers        - (lint) check source files for the license disclaimer
  migrations     - (lint) check migration sequence numbers
  misspell       - (lint) run misspell on source files
  npm            - run npm scripts from package.json
  script_hashes  - (lint) check script hashes
  script_output  - (lint) check script output
  staticcheck    - (lint) run staticcheck on source files
  unparam        - (lint) run unparam on source files
  prettier       - (lint, nonstandard) run prettier on .js and .css files.
  templates      - (lint, nonstandard) run go-template-lint on templates
EOUSAGE
}

# Packages to run without the race detector on CI.
# (They time out with -race.)
declare -A no_race
no_race=(
  [golang.org/x/pkgsite/internal/frontend]=1
  [golang.org/x/pkgsite/internal/worker]=1
  [golang.org/x/pkgsite/internal/testing/integration]=1
)

main() {
  case "$1" in
    "-h" | "--help" | "help")
      usage
      exit 0
      ;;
    "")
      standard_linters
      run_prettier
      npm run lint
      npm run test
      runcmd go mod tidy
      runcmd env GO_DISCOVERY_TESTDB=true go test ./...
      runcmd go test ./internal/secrets
      ;;
    cl)
      # Similar to the above, but only run checks that apply to files in this commit.
      files=$(modified_files)
      if [[ $files = '' ]]; then
        info "No modified files; nothing to do."
        exit 0
      fi
      info "Running checks on:"
      info "    " $files

      check_headers $(filter "$files" '*.go' '*.sql' '*.sh')
      if [[ $(filter "$files" 'migrations/*') != '' ]]; then
        check_bad_migrations
      fi
      if [[ $(filter "$files" $script_hash_glob) != '' ]]; then
        check_script_hashes
        check_script_output
      fi
      if [[ $(filter "$files" '*.go') != '' ]]; then
        go_linters
      fi
      pfiles=$(filter "$files" $prettier_file_globs)
      if [[ $pfiles != '' ]]; then
        run_prettier $pfiles
      fi
      if [[ $(filter "$files" 'content/static/**') != '' ]]; then
        npm run lint
        npm run test
      fi
      runcmd go mod tidy
      runcmd env GO_DISCOVERY_TESTDB=true go test ./...
      runcmd go test ./internal/secrets
      ;;

    ci)
      # Similar to the no-arg mode, but omit actions that require GCP
      # permissions or that don't test the code.
      # Also, run the race detector on most tests.
      standard_linters
      for pkg in $(go list ./...); do
        if [[ ${no_race[$pkg]} = '' ]]; then
          race="$race $pkg"
        fi
      done
      runcmd env GO_DISCOVERY_TESTDB=true go test -race -count=1 $race
      runcmd env GO_DISCOVERY_TESTDB=true go test -count=1 ${!no_race[*]}
      ;;
    lint) standard_linters ;;
    headers) check_headers ;;
    migrations) check_migrations ;;
    misspell) check_misspell ;;
    staticcheck) check_staticcheck ;;
    prettier) run_prettier ;;
    templates) check_templates ;;
    unparam) check_unparam ;;
    script_hashes) check_script_hashes ;;
    script_output) check_script_output ;;
    npm) npm ${@:2} ;;
    e2e) run_e2e ${@:2} ;;
    *)
      usage
      exit 1
  esac
  if [[ $EXIT_CODE != 0 ]]; then
    err "FAILED; see errors above"
  fi
  exit $EXIT_CODE
}

main $@
