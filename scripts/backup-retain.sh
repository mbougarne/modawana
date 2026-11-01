#!/bin/sh
# Retain seven daily and four weekly encrypted bundles in one dedicated directory.
set -eu
root=${1:?dedicated backup directory required}
case "$root" in /*) ;; *) exit 2 ;; esac
for spec in daily:7 weekly:4; do
    folder=${spec%:*}
    keep=${spec#*:}
    count=0
    for name in $(LC_ALL=C find "$root/$folder" -maxdepth 1 -type f -name '*.zip.age' -printf '%f\n' | LC_ALL=C sort -r); do
        case "$name" in
            [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9].zip.age|[0-9][0-9][0-9][0-9]-W[0-9][0-9].zip.age) ;;
            *) continue ;;
        esac
        count=$((count + 1))
        if [ "$count" -gt "$keep" ]; then
            rm -- "$root/$folder/$name"
        fi
    done
done
