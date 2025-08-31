package controller

import (
	"bufio"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	//logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//import (
//)
//
//func GetReleaseImages(releaseImage string) ([]string, error) {
//	o := release.InfoOptions{
//		Images: []string{releaseImage},
//	}
//
//	err := o.Run()
//	if err != nil {
//		return nil, err
//	}
//	return o.Images, nil
//}

// in the meantime because I am dumb, just run the cli command

func runOCCommand(releaseImage string) (outputLinesStr string, err error) {
	outputLines := []string{}
	outputLinesStr = ""

	cmdLine := "oc adm release info " + releaseImage + " -o jsonpath='{.references.spec.tags}' | jq -r '[.[] | {name: .name, from: .from.name}]'"

	cmd := exec.Command("/bin/sh", "-c", cmdLine)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(stdout)
	// Bigger buffer size for scanner
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	err = cmd.Start()
	if err != nil {
		return "", err
	}

	for scanner.Scan() {
		// Do something with the line here.
		outputLines = append(outputLines, scanner.Text())
	}
	if scanner.Err() != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return "", scanner.Err()
	}
	// Join the lines together
	outputLinesStr = strings.Join(outputLines, "")
	return outputLinesStr, cmd.Wait()
}

func GetReleaseImages(releaseImage string) ([]ReleaseImageTags, error) {
	outputLines, err := runOCCommand(releaseImage)
	if err != nil {
		return []ReleaseImageTags{}, err
	}

	// Parse the output as json
	var jsonData []ReleaseImageTags
	if err := json.Unmarshal([]byte(outputLines), &jsonData); err != nil {
		return []ReleaseImageTags{}, err
	}
	return jsonData, nil
}

func FilterReleaseImages(releaseImages []ReleaseImageTags, infrastructureType string) []ReleaseImageTags {
	var filteredReleaseImageTags []ReleaseImageTags
	var filterMatch *regexp.Regexp

	// Switch between the infrastructure types and set the filter for each
	switch infrastructureType {
	case "None", "BareMetal":
		// Removal of aws- azure- gcp- ibm- ibmcloud- libvirt- nutanix- openstack- ovirt- powervs- vsphere-
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "AWS":
		filterMatch = regexp.MustCompile(`azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "Azure":
		filterMatch = regexp.MustCompile(`aws-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "GCP":
		filterMatch = regexp.MustCompile(`aws-|azure-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "IBMCloud":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|libvirt-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "Libvirt":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|nutanix-|openstack-|ovirt-|powervs-|vsphere-`)
	case "Nutanix":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|openstack-|ovirt-|powervs-|vsphere-`)
	case "OpenStack":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|ovirt-|powervs-|vsphere-`)
	case "oVirt":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|powervs-|vsphere-`)
	case "PowerVS":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|vsphere-`)
	case "VSphere":
		filterMatch = regexp.MustCompile(`aws-|azure-|gcp-|ibm-|ibmcloud-|libvirt-|nutanix-|openstack-|ovirt-|powervs-`)
	}

	// If the Filter is empty, just return all the images
	if filterMatch == nil {
		return releaseImages
	}

	// Otherwise step through all the images and check the name
	for _, item := range releaseImages {
		if !filterMatch.MatchString(item.Name) {
			filteredReleaseImageTags = append(filteredReleaseImageTags, item)
		}
	}
	return filteredReleaseImageTags
}
