#!/bin/bash

CTR_RUNTIME="crictl" # or podman
RELEASE_VERSION=${1}

INFRA_TYPE=$(oc get infrastructure/cluster -o jsonpath='{.status.platformStatus.type}')

RELEASE_INFO=$(oc adm release info ${RELEASE_VERSION} -o json)
RELEASE_IMAGE=$(echo $RELEASE_INFO | jq -rMc .image)

case "${INFRA_TYPE}" in
  "None")
    INFRA_FILTER='(contains("aws-") | not) and (contains("azure-") | not) and (contains("vsphere-") | not) and (contains("powervs-") | not) and (contains("nutanix-") | not) and (contains("ovirt-") | not) and (contains("openstack-") | not) and (contains("ibm-") | not) and (contains("ibmcloud-") | not) and (contains("libvirt-") | not) and (contains("gcp-") | not)'
    ;;
  "VSphere")
    INFRA_FILTER='(contains("aws-") | not) and (contains("azure-") | not) and (contains("powervs-") | not) and (contains("nutanix-") | not) and (contains("ovirt-") | not) and (contains("openstack-") | not) and (contains("ibm-") | not) and (contains("ibmcloud-") | not) and (contains("libvirt-") | not) and (contains("gcp-") | not)'
    ;;
  "Azure")
    INFRA_FILTER='(contains("aws-") | not) and (contains("vsphere-") | not) and (contains("powervs-") | not) and (contains("nutanix-") | not) and (contains("ovirt-") | not) and (contains("openstack-") | not) and (contains("ibm-") | not) and (contains("ibmcloud-") | not) and (contains("libvirt-") | not) and (contains("gcp-") | not)'
    ;;
  "AWS")
    INFRA_FILTER='(contains("azure-") | not) and (contains("vsphere-") | not) and (contains("powervs-") | not) and (contains("nutanix-") | not) and (contains("ovirt-") | not) and (contains("openstack-") | not) and (contains("ibm-") | not) and (contains("ibmcloud-") | not) and (contains("libvirt-") | not) and (contains("gcp-") | not)'
    ;;
  *)
    INFRA_FILTER='(contains("dummy-") | not)'
    ;;
esac

RELEASE_IMAGES=$(echo ${RELEASE_INFO} | jq -r ".references.spec.tags | .[] | select(.name | ${INFRA_FILTER}) | \"\(.from.name)\"")

echo "Target OpenShift Version: $RELEASE_VERSION"
echo "Release Image: $RELEASE_IMAGE"
echo "Infrastructure Provider: ${INFRA_TYPE}"
echo "Pulling release image..."
$CTR_RUNTIME pull $RELEASE_IMAGE

string_array=( $RELEASE_IMAGES )
for img in "${string_array[@]}"; do
  echo "Pulling $img"
  $CTR_RUNTIME pull $img
done
