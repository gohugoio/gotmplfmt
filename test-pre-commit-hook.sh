#!/usr/bin/env bash

set -o errtrace -o nounset -o pipefail -o errexit

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
MOCK_REPO="$(mktemp -d)"

cleanup() {
	rm -rf "$MOCK_REPO"
}

trap cleanup EXIT

init_repo() {
	local repo_dir="$1"
	mkdir -p "$repo_dir"
	pushd "$repo_dir" >/dev/null
	git init --initial-branch=main
	git config user.email "test@example.com"
	git config user.name "pre-commit test"
	popd >/dev/null
}

run_hook_expect_changes() {
	local repo_dir="$1"
	local hook_id="$2"

	pushd "$repo_dir" >/dev/null
	set +e
	pre-commit try-repo "$SCRIPT_DIR" "$hook_id" --verbose --all-files
	status=$?
	set -e

	if [[ $status -eq 0 ]]; then
		echo "expected $hook_id to report modifications"
		exit 1
	fi
	popd >/dev/null
}

run_hook_expect_success() {
	local repo_dir="$1"
	local hook_id="$2"

	pushd "$repo_dir" >/dev/null
	pre-commit try-repo "$SCRIPT_DIR" "$hook_id" --verbose --all-files
	popd >/dev/null
}

single_repo="$MOCK_REPO/single"
init_repo "$single_repo"
pushd "$single_repo" >/dev/null

cp "$SCRIPT_DIR/internal/format/golden/in/else-if.html" index.html
cp "$SCRIPT_DIR/internal/format/golden/out/else-if.html" expected.html

git add .
git commit -m "Initial commit"
popd >/dev/null

run_hook_expect_changes "$single_repo" gotmplfmt

pushd "$single_repo" >/dev/null
cmp index.html expected.html
popd >/dev/null

recursive_repo="$MOCK_REPO/recursive"
init_repo "$recursive_repo"
pushd "$recursive_repo" >/dev/null
mkdir -p templates
cp "$SCRIPT_DIR/internal/format/golden/out/else-if.html" templates/page.html

git add .
git commit -m "Initial commit"
popd >/dev/null

run_hook_expect_success "$recursive_repo" gotmplfmt-recursive

pushd "$recursive_repo" >/dev/null
cmp templates/page.html "$SCRIPT_DIR/internal/format/golden/out/else-if.html"
popd >/dev/null
