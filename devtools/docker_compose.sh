#!/usr/bin/env bash

# Copyright 2020 The Go Authors. All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

source devtools/lib.sh || { echo "Are you at repo root?"; exit 1; }

if [[ ! -x "$(command -v docker-compose)" ]]; then
  err "docker-compose must be installed: see https://docs.docker.com/compose/install/"
  exit 1
fi

usage() {
  cat <<EOUSAGE
Usage: $0 {--build} [nodejs|ci] {command}

Run services using docker-compose. Used to build, lint, and test static assets and run
e2e tests.

EOUSAGE
}

function cleanup() {
  info "Cleaning up..."
  runcmd docker-compose -f devtools/config/docker-compose.yaml down --remove-orphans --rmi local
}

run_build=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    "-h" | "--help" | "help")
      usage
      exit 0
      ;;
    "--build")
      run_build=true
      shift
      ;;
    "logs")
      runcmd docker-compose -f devtools/config/docker-compose.yaml logs -f
      exit $EXIT_CODE
      ;;
    "nodejs" | "ci")
      trap cleanup EXIT SIGINT

      if $run_build; then
        docker-compose -f devtools/config/docker-compose.yaml build
      fi

      # Run an npm command and capture the exit code.
      runcmd docker-compose -f devtools/config/docker-compose.yaml run --rm $@

      # Exit with the code from the docker-compose command.
      exit $EXIT_CODE
      ;;
    *)
      usage
      exit 1
      ;;
  esac
done

