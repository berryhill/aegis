#!/bin/sh
# Acquire a verified private coordinator; never replace the installed image here.
set -eu
exec python3 "$(dirname "$0")/bootstrap-installed-update.py" "$@"
