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
3. Run `make release`
4. Add/Commit changes
5. Push the new tag to Git

## 3. Adding new release version to Operator Catalog

The Operator is distributed as part of the Kemo OpenShift Operator Catalog https://github.com/kenmoini/openshift-operator-catalog

```bash
oc apply -k https://github.com/kenmoini/openshift-operator-catalog/deploy/overlays/stable/
```

Operator Bundles can be easily included in this catalog simply by creating/editing a YAML file in the `bundles/` directory of that repo.

Once the changes are merged into either the main or stable branches, is a semver tag that starts with `v*` the GitHub Actions workflows will build the operator catalog and push.
