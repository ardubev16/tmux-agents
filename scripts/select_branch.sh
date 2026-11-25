#!/usr/bin/env bash

. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/_common.sh"

# Esc/Ctrl-C return 130 (cancelled); `||` keeps that from tripping the error trap, main.sh treats 130 as cancel.
select_branch "$1" >"$2" || exit $?
