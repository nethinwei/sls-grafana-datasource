#!/usr/bin/env bash
# Copies the license and notice files of every Go module compiled into the backend into $1
# (one directory per module@version), so the release package carries them. Uses only the Go
# toolchain and the module cache. Fails when a module has no license file.
set -euo pipefail
out=${1:?usage: collect-go-licenses.sh <output dir>}
mkdir -p "$out"
shopt -s nullglob nocaseglob
missing=0
while read -r mod dir; do
  files=("$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*)
  if [ ${#files[@]} -eq 0 ]; then
    echo "no license file: $mod" >&2
    missing=1
    continue
  fi
  mkdir -p "$out/$mod"
  cp "${files[@]}" "$out/$mod/"
done < <(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}} {{.Dir}}{{end}}{{end}}' ./pkg | sort -u)
exit $missing
