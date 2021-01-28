#!/bin/bash

# Update submodules HEAD to remote branches

set -euo pipefail

function help() {
    echo "Usage: update_submodules.sh [OPTIONS]... branch_name..."
    echo "Update submodules HEAD to remote branches"
    echo "Must be ran in repo root directory"
    echo ""
    echo "Options:"
    echo "-h   Shows usage text"
    echo "Example:"
    echo "update_submodules.sh master release-4.6"
}

function update_branch_submodules() {
    branch=$1
    working_branch_name="$branch-SubmoduleUpdate$(date +%m-%d)"
    echo Using branch $working_branch_name
    git branch -D $working_branch_name || true
    git checkout $branch -b $working_branch_name
    git submodule update --remote

    # The submodules have been updated at this point, now a commit needs to be generated for each of them
    git config --file .gitmodules --get-regexp path | awk '{ print $2 }' | while read submodule; do
        cd $submodule
        origin_url=$(git remote get-url origin)
        short_head=$(git rev-parse --short HEAD)
        long_head=$(git rev-parse HEAD)
        cd ..
        git add $submodule
	# Commit changes if there are any
        git commit -m "[submodule][$submodule] Bump to $short_head" -m "Bump to $origin_url/commit/$long_head" -m "This commit was generated using hack/update_submodules.sh" || true
    done
}

while getopts ":d:u:i:h" opt; do
    case "$opt" in
    h) help; exit 0;;
    ?) help; exit 1;;
    esac
done
shift $((OPTIND -1))

if ! git diff --quiet; then
  echo "branch dirty, exiting to not overwrite work"
  exit 1
fi
for branch in "$@"; do
    echo "Generating submodule commits "
    update_branch_submodules $branch
done
