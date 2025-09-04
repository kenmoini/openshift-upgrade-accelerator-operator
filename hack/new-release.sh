#!/bin/bash

# This script lets you quickly set up a new release from the main integration branch

# Usage:
#   ./hack/new-release.sh [-f] <new-version>

if [ -z "$1" ]; then
  echo "Error: No version specified."
  exit 1
fi

# Read in all the args
FORCE=false
while getopts "f" opt; do
  case $opt in
    f)
      FORCE=true
      ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      exit 1
      ;;
  esac
done
shift $((OPTIND - 1))

NEW_VERSION="$1"

# Create a new branch
if [ "$FORCE" = true ]; then
  git tag v${NEW_VERSION} --force
else
  git tag v${NEW_VERSION}
fi

# Replace the VERSION in the Makefile
sed -i.bak "s/^VERSION ?= .*/VERSION ?= ${NEW_VERSION}/" Makefile

# Commit the changes
git add Makefile
git commit -m "Release version ${NEW_VERSION}"

# Make the release manifests
make release

# Add the changes
git add bundle/
git add config/
git commit -m "Update release manifests for version ${NEW_VERSION}"

# Push the changes
git push origin v${NEW_VERSION}
