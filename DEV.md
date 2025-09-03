

```bash=
GOPROXY=direct GOSUMDB=off operator-sdk init --domain kemo.dev --repo github.com/kenmoini/openshift-upgrade-accelerator-operator

git init
git commit -m "first commit"
git branch -M main
git remote add origin git@github.com:kenmoini/openshift-upgrade-accelerator-operator.git
git push -u origin main

GOPROXY=direct GOSUMDB=off operator-sdk create api --group openshift --version v1alpha1 --kind UpgradeAccelerator --namespaced=false --resource --controller

make generate
make manifests
make lint

# Local dev
make generate manifests build install run

# Create Operator Image
make docker-build
make docker-push
# (or with GitHub actions)

# Create Operator Bundle
```

```yaml=
# Used when doing local testing
---
kind: RoleBinding
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: default-priv-test
  namespace: openshift-upgrade-accelerator
subjects:
  - kind: ServiceAccount
    name: default
    namespace: openshift-upgrade-accelerator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: 'system:openshift:scc:privileged'
```