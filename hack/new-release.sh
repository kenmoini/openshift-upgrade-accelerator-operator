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

# Checkout the release branch
git checkout -b release/v${NEW_VERSION} main

# Replace the VERSION in the Makefile
sed -i.bak "s/^VERSION ?= .*/VERSION ?= ${NEW_VERSION}/" Makefile

# Commit the changes
git add Makefile
git commit -m "RELEASE: Makefile version ${NEW_VERSION}"

# Make the release manifests
make release

# Add the changes
git add bundle/
git add config/
git commit -m "RELEASE: Manifests version ${NEW_VERSION}"

# Create a new tag
if [ "$FORCE" = true ]; then
  git tag v${NEW_VERSION} --force
else
  git tag v${NEW_VERSION}
fi

# Push the changes

if [ "$FORCE" = true ]; then
  git push origin v${NEW_VERSION} --force
else
  git push origin v${NEW_VERSION}
fi