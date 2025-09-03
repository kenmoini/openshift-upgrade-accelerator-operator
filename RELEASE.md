# Releasing

## Resources

- **Operator Image Repo:** quay.io/kenmoini/openshift-upgrade-accelerator-operator
- **Operator Bundle Image Repo:** quay.io/kenmoini/openshift-upgrade-accelerator-operator-bundle
- **Operator Catalog Repo:** quay.io/kenmoini/openshift-operator-catalog:stable

## Branching Strategy

- The development branch is `main`
- Versioned releases are done via semver and Tags, eg `v0.0.1`

## GitHub Actions

- Go Lint
- Go Tests
- Build Operator on main
- Build Bundle on main
- Release Operator on tagged v*
- Release Bundle on tagged v*

## 1. Building the Operator

1. Make changes
2. Tests (lol)
3. Lint

## 2. Versioning the Operator

1. Create a new Git tag for the release version `v0.0.2`
2. Change VERSION in the Makefile to reflect `0.0.2`
3. Run `make generate manifests bundle`
4. Add/Commit changes
5. Push the new tag to Git

## 3. Adding new release version to Operator Catalog
