#!/usr/bin/env bash
# Runs one shard of the race-enabled PostgreSQL integration suite (the
# api-postgres check). Run from api/ with GK_TEST_DATABASE_URL set.
#
#   integration-shard.sh rest              every package except internal/checkout
#   integration-shard.sh checkout K N      shard K (1..N) of internal/checkout
#
# Checkout's top-level tests are listed from its *_test.go files and dealt out
# round-robin by name. Shards 1..N-1 run their own names with -run; shard N
# runs everything the other shards do not (-skip), so a test the listing
# misses still runs exactly once. Each shard keeps -p 1: migrations create
# cluster-wide roles, so packages must not migrate concurrently on one cluster.
set -euo pipefail

flags=(-race -tags=integration -p 1 -count=1 -timeout 15m)
checkout=./internal/checkout

case "${1:-}" in
rest)
	# List with the build tag the tests use: packages whose files are all
	# integration-only (internal/dbtest) are invisible without it. A failing
	# go list stops the script (set -e covers the assignment, not a < <() list).
	all=$(go list -tags=integration ./...)
	pkgs=()
	while IFS= read -r p; do
		[[ $p == */internal/checkout ]] || pkgs+=("$p")
	done <<<"$all"
	if ((${#pkgs[@]} == 0)); then
		echo "no packages found" >&2
		exit 1
	fi
	go test "${flags[@]}" "${pkgs[@]}"
	;;
checkout)
	k=${2:?shard number}
	n=${3:?shard count}
	if ! [[ $k =~ ^[0-9]+$ && $n =~ ^[0-9]+$ ]] || ((k < 1 || k > n)); then
		echo "usage: $0 checkout K N (1 <= K <= N)" >&2
		exit 2
	fi
	listed=$(sed -nE 's/^func (Test[A-Za-z0-9_]*)\(t \*testing\.T\) \{$/\1/p' "$checkout"/*_test.go | LC_ALL=C sort -u)
	names=()
	while IFS= read -r name; do
		[[ -z $name ]] || names+=("$name")
	done <<<"$listed"
	if ((${#names[@]} == 0)); then
		echo "no tests found in $checkout" >&2
		exit 1
	fi
	mine='' others=''
	for i in "${!names[@]}"; do
		if ((i % n == k - 1)); then
			mine+="${mine:+|}${names[$i]}"
		else
			others+="${others:+|}${names[$i]}"
		fi
	done
	if ((k < n)); then
		echo "checkout shard $k/$n runs: ${mine//|/ }"
		go test "${flags[@]}" -run "^(${mine})\$" "$checkout"
	elif [[ -n $others ]]; then
		echo "checkout shard $k/$n skips the other shards' tests and runs the rest"
		go test "${flags[@]}" -skip "^(${others})\$" "$checkout"
	else
		go test "${flags[@]}" "$checkout"
	fi
	;;
*)
	echo "usage: $0 rest | checkout K N" >&2
	exit 2
	;;
esac
