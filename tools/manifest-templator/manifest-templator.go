/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/ghodss/yaml"
	"github.com/spf13/pflag"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	cnacomponents "github.com/kubevirt/cluster-network-addons-operator/pkg/components"
	hcocomponents "github.com/kubevirt/hyperconverged-cluster-operator/pkg/components"
	kwebuicomponents "github.com/kubevirt/web-ui-operator/pkg/components"
	extv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	cdicomponents "kubevirt.io/containerized-data-importer/pkg/operator/resources/operator"
	kvcomponents "kubevirt.io/kubevirt/pkg/virt-operator/creation/components"
	kvrbac "kubevirt.io/kubevirt/pkg/virt-operator/creation/rbac"
)

type operatorData struct {
	Deployment        string
	DeploymentSpec    string
	RoleString        string
	Rules             string
	ClusterRoleString string
	ClusterRules      string
	CRD               *extv1beta1.CustomResourceDefinition
	CRDString         string
	CRString          string
	Tag               string
}

type templateData struct {
	Converged            bool
	Namespace            string
	CsvVersion           string
	ContainerPrefix      string
	CnaContainerPrefix   string
	WebuiContainerPrefix string
	ImagePullPolicy      string
	CreatedAt            string
	HCO                  *operatorData
	KubeVirt             *operatorData
	CDI                  *operatorData
	CNA                  *operatorData
	KWEBUI               *operatorData
	SSP                  *operatorData
	NMO                  *operatorData
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func fixResourceString(in string, indention int) string {
	out := strings.Builder{}
	scanner := bufio.NewScanner(strings.NewReader(in))
	for scanner.Scan() {
		line := scanner.Text()
		// remove separator lines
		if !strings.HasPrefix(line, "---") {
			// indent so that it fits into the manifest
			// spaces is is indention - 2, because we want to have 2 spaces less for being able to start an array
			spaces := strings.Repeat(" ", indention-2)
			if strings.HasPrefix(line, "apiGroups") {
				// spaces + array start
				out.WriteString(spaces + "- " + line + "\n")
			} else {
				// 2 more spaces
				out.WriteString(spaces + "  " + line + "\n")
			}
		}
	}
	return out.String()
}

func marshallObject(obj interface{}, writer io.Writer) error {
	jsonBytes, err := json.Marshal(obj)
	check(err)

	var r unstructured.Unstructured
	if err := json.Unmarshal(jsonBytes, &r.Object); err != nil {
		return err
	}

	// remove status and metadata.creationTimestamp
	unstructured.RemoveNestedField(r.Object, "template", "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(r.Object, "status")

	jsonBytes, err = json.Marshal(r.Object)
	if err != nil {
		return err
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return err
	}

	// fix templates by removing quotes...
	s := string(yamlBytes)
	s = strings.Replace(s, "'{{", "{{", -1)
	s = strings.Replace(s, "}}'", "}}", -1)
	yamlBytes = []byte(s)

	_, err = writer.Write([]byte("---\n"))
	if err != nil {
		return err
	}

	_, err = writer.Write(yamlBytes)
	if err != nil {
		return err
	}

	return nil
}

func getHCO(data *templateData) {
	writer := strings.Builder{}

	// Get HCO Deployment
	hcodeployment := hcocomponents.GetDeployment(
		"quay.io",
		data.HCO.Tag,
		"Always",
	)
	err := marshallObject(hcodeployment, &writer)
	check(err)
	deployment := writer.String()

	// Get HCO DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(hcodeployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get HCO ClusterRole
	writer = strings.Builder{}
	clusterRole := hcocomponents.GetClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of HCO's ClusterRole
	writer = strings.Builder{}
	hcorules := clusterRole.Rules
	for _, rule := range hcorules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get HCO CRD
	writer = strings.Builder{}
	crd := hcocomponents.GetCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	// Get HCO CR
	writer = strings.Builder{}
	cr := hcocomponents.GetCR()
	marshallObject(cr, &writer)
	crString := writer.String()

	data.HCO.Deployment = deployment
	data.HCO.DeploymentSpec = deploymentSpec
	data.HCO.ClusterRoleString = clusterRoleString
	data.HCO.Rules = rules
	data.HCO.CRD = crd
	data.HCO.CRDString = crdString
	data.HCO.CRString = crString
}

func getKubeVirt(data *templateData) {
	writer := strings.Builder{}

	// Get KubeVirt Operator Deployment
	kvdeployment, err := kvcomponents.NewOperatorDeployment(
		"kubevirt",
		data.ContainerPrefix,
		data.KubeVirt.Tag,
		v1.PullPolicy(data.ImagePullPolicy),
		"2",
	)
	kvdeployment.ObjectMeta.Namespace = ""
	check(err)
	err = marshallObject(kvdeployment, &writer)
	check(err)
	deployment := writer.String()

	// Get KubeVirt DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(kvdeployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get KubeVirt ClusterRole
	writer = strings.Builder{}
	clusterRole := kvrbac.NewOperatorClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of KubeVirt's ClusterRole
	writer = strings.Builder{}
	kvrules := clusterRole.Rules
	for _, rule := range kvrules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get KubeVirt CRD
	writer = strings.Builder{}
	crd := kvcomponents.NewKubeVirtCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	data.KubeVirt.Deployment = deployment
	data.KubeVirt.DeploymentSpec = deploymentSpec
	data.KubeVirt.ClusterRoleString = clusterRoleString
	data.KubeVirt.Rules = rules
	data.KubeVirt.CRD = crd
	data.KubeVirt.CRDString = crdString
}

func getCDI(data *templateData) {
	writer := strings.Builder{}

	// Get CDI Deployment
	cdideployment, err := cdicomponents.NewCdiOperatorDeployment(
		data.Namespace,
		"kubevirt",
		"v1.9.1",
		"IfNotPresent",
		"1",
		(&cdicomponents.CdiImages{}).FillDefaults())

	check(err)
	err = marshallObject(cdideployment, &writer)
	check(err)
	deployment := writer.String()

	// Get CDI DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(cdideployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get CDI ClusterRole
	writer = strings.Builder{}
	clusterRole := cdicomponents.NewCdiOperatorClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of CDI's ClusterRole
	writer = strings.Builder{}
	cdirules := clusterRole.Rules
	for _, rule := range cdirules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get HCO CRD
	writer = strings.Builder{}
	crd := cdicomponents.NewCdiCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	data.CDI.Deployment = deployment
	data.CDI.DeploymentSpec = deploymentSpec
	data.CDI.ClusterRoleString = clusterRoleString
	data.CDI.Rules = rules
	data.CDI.CRD = crd
	data.CDI.CRDString = crdString
}

func getCNA(data *templateData) {
	writer := strings.Builder{}

	// Get CNA Deployment
	cnadeployment := cnacomponents.GetDeployment(
		"0.11.0",
		data.Namespace,
		data.CnaContainerPrefix,
		"0.11.0",
		data.ImagePullPolicy,
		(&cnacomponents.AddonsImages{}).FillDefaults(),
	)
	err := marshallObject(cnadeployment, &writer)
	check(err)
	deployment := writer.String()

	// Get CNA DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(cnadeployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get CNA Role
	writer = strings.Builder{}
	role := cnacomponents.GetRole(data.Namespace)
	marshallObject(role, &writer)
	roleString := writer.String()

	// Get the Rules out of CNA's ClusterRole
	writer = strings.Builder{}
	cnaRules := role.Rules
	for _, rule := range cnaRules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get CNA ClusterRole
	writer = strings.Builder{}
	clusterRole := cnacomponents.GetClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of CNA's ClusterRole
	writer = strings.Builder{}
	cnaClusterRules := clusterRole.Rules
	for _, rule := range cnaClusterRules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	clusterRules := fixResourceString(writer.String(), 14)

	// Get CNA CRD
	writer = strings.Builder{}
	crd := cnacomponents.GetCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	data.CNA.Deployment = deployment
	data.CNA.DeploymentSpec = deploymentSpec
	data.CNA.RoleString = roleString
	data.CNA.Rules = rules
	data.CNA.ClusterRoleString = clusterRoleString
	data.CNA.ClusterRules = clusterRules
	data.CNA.CRD = crd
	data.CNA.CRDString = crdString
}

func getKWEBUI(data *templateData) {
	writer := strings.Builder{}

	// Get KWEBUI Deployment
	kwebuideployment := kwebuicomponents.GetDeployment(
		data.Namespace,
		data.WebuiContainerPrefix,
		data.KWEBUI.Tag,
		data.ImagePullPolicy,
	)
	err := marshallObject(kwebuideployment, &writer)
	check(err)
	deployment := writer.String()

	// Get KWebUI DeploymentSpec for CSV
	writer = strings.Builder{}
	err = marshallObject(kwebuideployment.Spec, &writer)
	check(err)
	deploymentSpec := fixResourceString(writer.String(), 12)

	// Get KWebUI Role
	writer = strings.Builder{}
	role := kwebuicomponents.GetRole(data.Namespace)
	marshallObject(role, &writer)
	roleString := writer.String()

	// Get the Rules out of KWebUI's ClusterRole
	writer = strings.Builder{}
	kwebuiRules := role.Rules
	for _, rule := range kwebuiRules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	rules := fixResourceString(writer.String(), 14)

	// Get KWebUI ClusterRole
	writer = strings.Builder{}
	clusterRole := kwebuicomponents.GetClusterRole()
	marshallObject(clusterRole, &writer)
	clusterRoleString := writer.String()

	// Get the Rules out of KWebUI's ClusterRole
	writer = strings.Builder{}
	kwebuiClusterRules := clusterRole.Rules
	for _, rule := range kwebuiClusterRules {
		err := marshallObject(rule, &writer)
		check(err)
	}
	clusterRules := fixResourceString(writer.String(), 14)

	// Get KWebUI CRD
	writer = strings.Builder{}
	crd := kwebuicomponents.GetCrd()
	marshallObject(crd, &writer)
	crdString := writer.String()

	data.KWEBUI.Deployment = deployment
	data.KWEBUI.DeploymentSpec = deploymentSpec
	data.KWEBUI.RoleString = roleString
	data.KWEBUI.Rules = rules
	data.KWEBUI.ClusterRoleString = clusterRoleString
	data.KWEBUI.ClusterRules = clusterRules
	data.KWEBUI.CRD = crd
	data.KWEBUI.CRDString = crdString
}

func main() {
	converged := flag.Bool("converged", false, "")
	namespace := flag.String("namespace", "kubevirt-hyperconverged", "")
	csvVersion := flag.String("csv-version", "0.0.1", "")
	containerPrefix := flag.String("container-prefix", "kubevirt", "")
	cnaContainerPrefix := flag.String("cna-container-prefix", *containerPrefix, "")
	webuiContainerPrefix := flag.String("webui-container-prefix", *containerPrefix, "")
	imagePullPolicy := flag.String("image-pull-policy", "IfNotPresent", "")
	inputFile := flag.String("input-file", "", "")

	containerTag := flag.String("container-tag", "latest", "")
	hcoTag := flag.String("hco-tag", *containerTag, "")
	kubevirtTag := flag.String("kubevirt-tag", *containerTag, "")
	cdiTag := flag.String("cdi-tag", *containerTag, "")
	sspTag := flag.String("ssp-tag", *containerTag, "")
	webUITag := flag.String("web-ui-tag", *containerTag, "")
	nmoTag := flag.String("nmo-tag", *containerTag, "")
	networkAddonsTag := flag.String("network-addons-tag", *containerTag, "")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.CommandLine.ParseErrorsWhitelist.UnknownFlags = true
	pflag.Parse()

	data := templateData{
		Converged:            *converged,
		Namespace:            *namespace,
		CsvVersion:           *csvVersion,
		ContainerPrefix:      *containerPrefix,
		CnaContainerPrefix:   *cnaContainerPrefix,
		WebuiContainerPrefix: *webuiContainerPrefix,
		ImagePullPolicy:      *imagePullPolicy,

		HCO:      &operatorData{Tag: *hcoTag},
		KubeVirt: &operatorData{Tag: *kubevirtTag},
		CDI:      &operatorData{Tag: *cdiTag},
		CNA:      &operatorData{Tag: *networkAddonsTag},
		KWEBUI:   &operatorData{Tag: *webUITag},
		SSP:      &operatorData{Tag: *sspTag},
		NMO:      &operatorData{Tag: *nmoTag},
	}
	data.CreatedAt = time.Now().String()

	// Load in all HCO Resources
	getHCO(&data)
	// Load in all of the KubeVirt Resources
	getKubeVirt(&data)
	// Load in all CDI Resources
	getCDI(&data)
	// Load in all CNA Resources
	getCNA(&data)
	// Load in all KWEBUI Resources
	getKWEBUI(&data)

	if *inputFile == "" {
		panic("Must specify input file")
	}

	manifestTemplate := template.Must(template.ParseFiles(*inputFile))
	err := manifestTemplate.Execute(os.Stdout, data)
	check(err)
}
