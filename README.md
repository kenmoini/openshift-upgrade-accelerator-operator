# OpenShift Upgrade Accelerator Operator

[![Lint](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/lint.yml) [![Tests](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/test.yml) [![Operator - Build](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/build-container.yml/badge.svg)](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/build-container.yml) [![Bundle - Build](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/build-bundle.yml/badge.svg)](https://github.com/kenmoini/openshift-upgrade-accelerator-operator/actions/workflows/build-bundle.yml)

> This is a community operator, unsupported by Red Hat.  Support is tired of hearing about me.

The OpenShift Upgrade Accelerator Operator reduces the time required for OpenShift upgrades by pre-pulling release images onto Nodes - this way they're ready as soon as the Pod is ready to be scheduled, and the Upgrade can progress through its states faster.

## Description

During an OpenShift upgrade, there are numerous processes that need to occur in the right order.  API, etcd, kubelet, etc updates - then to node reboots - then to waiting for various ClusterOperators to come online.

A significant portion of the wait time during the upgrade process is spent in waiting for images to pull.  This delay can cause reconciliation loops with increasing back-off times while the images are pulled.

The OpenShift Upgrade Accelerator Operator aims to speed up the upgrade process by pulling images down to nodes before they're needed.  This way, the images are present on the nodes as soon as scheduling occurs.

The default event trigger is when the ClusterVersion changes to a new release.  The desired release is determined, the images from that release are pulled preemptively.

## Features

- Multi-Arch!  x86_64, Arm64, PPC64, x390x
- Disconnected mirroring support!
- Proxy-Aware!
- Primer Nodes - Pull release images to a single or set of nodes first before progressing to other nodes.  Useful when leveraging a local pull-through/proxy cache container image registry.
- Node Exclusions - Disable nodes from being randomly selected as a primer node, or from being processed at all.
- Manual release image overrides - Pre-prime images before an upgrade is even started, or pull a release from a difference build/source.

## Getting Started

To deploy this Operator in OpenShift, the easiest way to do so is by adding the Operator's CatalogSource to the cluster.  By doing so, the Operator will show up in the OperatorHub like any other Operator, and can be installed with traditional OperatorGroup/Subscription/InstallPlan/etc manifests.

```bash
# Deploy the CatalogSource
oc apply -k https://github.com/kenmoini/openshift-operator-catalog/deploy/overlays/stable/
```

## Contributing

A good way to contribute to this project is to check out the current open Issues and Pull Requests for anything that is currently in-flight.

Any bugs, enhancements, etc should be started with an Issue first to make discussion available over the impact of the request.

Once an Issue is open to track the discussion, you can optionally provide contributions with Pull Requests.

To do so, fork this repo, then make a new branch in your fork to track changes.  Commit them to that branch, push to your fork, and then open a Pull Request from there to merge into `main` which is the primary branch.

Once changes have been merged into main, a versioned release can occur to distribute it and other changes.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

---

## Overrides

### Disabling Per-Node Preheating

In case you want to cast a wide net but let a few small fish through, you can label nodes with `openshift.kemo.dev/disable-preheat: "true"` and the Operator will **exclude that node no matter what** other selectors include it.  This will cause the node to only pull release images as it needs when normally scheduled.

### Manually Specifying Primer Nodes

If you want a node (or set of them) to be specified as Primer Nodes (nodes that pull images before all the others that are being targeted) you can apply a Node label of `openshift.kemo.dev/primer-node: "true"`.

Any node with that label will have the images pulled to it first - this is only done when Priming is enabled and ignores any Parallelism limits if you have multiple nodes labeled.

Inversely, set the label's value to `false` in case you want to exclude a Node from being a primer node.

---

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

