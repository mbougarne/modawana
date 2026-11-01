#!/bin/sh
# Linux operator entry point. Requires age, rsync, OpenSSH, GNU date/find and flock.
set -eu
umask 077
: "${BLOG_BACKUP_DIR:?private backup staging directory required}"
: "${BLOG_BACKUP_REMOTE:?user@host:/absolute/directory required}"
: "${BLOG_BACKUP_RECIPIENT:?age public recipient required}"
: "${BLOG_BINARY:?absolute blog executable required}"
case "$BLOG_BACKUP_DIR" in /*) ;; *) exit 2 ;; esac
case "$BLOG_BINARY" in /*) ;; *) exit 2 ;; esac
# Restrict remote command text to a deliberately small, non-shell path alphabet.
if ! printf '%s' "$BLOG_BACKUP_REMOTE" | LC_ALL=C grep -Eq '^[A-Za-z0-9][A-Za-z0-9_.-]*@[A-Za-z0-9][A-Za-z0-9.-]*:/[A-Za-z0-9/_-]+$'; then
    exit 2
fi
host=${BLOG_BACKUP_REMOTE%%:*}
remote=${BLOG_BACKUP_REMOTE#*:}
mkdir -p "$BLOG_BACKUP_DIR/daily" "$BLOG_BACKUP_DIR/weekly"
exec 9>"$BLOG_BACKUP_DIR/runner.lock"
flock -n 9
work=$(mktemp -d "$BLOG_BACKUP_DIR/.pending-XXXXXX")
trap 'rm -rf -- "$work"' EXIT HUP INT TERM
"$BLOG_BINARY" backup --file "$work/data.zip"
age -r "$BLOG_BACKUP_RECIPIENT" -o "$work/data.zip.age" "$work/data.zip"
day=$(date -u +%F)
week=$(date -u +%G-W%V)
mv -- "$work/data.zip.age" "$BLOG_BACKUP_DIR/daily/$day.zip.age"
if [ ! -f "$BLOG_BACKUP_DIR/weekly/$week.zip.age" ]; then
    cp -- "$BLOG_BACKUP_DIR/daily/$day.zip.age" "$BLOG_BACKUP_DIR/weekly/$week.zip.age"
fi
ssh -o BatchMode=yes "$host" "umask 077; mkdir -p '$remote/daily' '$remote/weekly'"
rsync -a --checksum -e 'ssh -o BatchMode=yes' -- "$BLOG_BACKUP_DIR/daily/$day.zip.age" "$BLOG_BACKUP_REMOTE/daily/"
rsync -a --checksum -e 'ssh -o BatchMode=yes' -- "$BLOG_BACKUP_DIR/weekly/$week.zip.age" "$BLOG_BACKUP_REMOTE/weekly/"
# Prune only after both transfers succeeded. The same reviewed script runs remotely.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ssh -o BatchMode=yes "$host" "sh -s -- '$remote'" < "$script_dir/backup-retain.sh"
sh "$script_dir/backup-retain.sh" "$BLOG_BACKUP_DIR"
printf '%s\n' 'Encrypted daily/weekly backups transferred; retention completed.'
