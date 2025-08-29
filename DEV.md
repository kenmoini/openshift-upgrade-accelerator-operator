

```bash=
GOPROXY=direct GOSUMDB=off operator-sdk init --domain kemo.dev --repo github.com/kenmoini/openshift-upgrade-accelerator-operator

git init
git commit -m "first commit"
git branch -M main
git remote add origin git@github.com:kenmoini/openshift-upgrade-accelerator-operator.git
git push -u origin main

operator-sdk create api --group openshift --version v1alpha1 --kind UpgradeAccelerator --namespaced=false --resource --controller

```
