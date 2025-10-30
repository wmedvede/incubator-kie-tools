// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements.  See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership.  The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License.  You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package common

import (
	"context"
	"fmt"
	"regexp"

	cncfmodel "github.com/serverlessworkflow/sdk-go/v2/model"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"k8s.io/klog/v2"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/workflowproj"
)

const (
	ServerlessWorkflowProjectJarPattern = "serverless-workflow-project-[\\w.-]+\\.jar$"
)

// ReadWorkflowFilesFromImage reads the image referred by the imageRef, and extracts all the workflow files existing in
// the jar file that matches the jarPattern, if any.
func ReadWorkflowFilesFromImage(ctx context.Context, cli kubernetes.Interface, imageRef, namespace, serviceAccount string, imagePullSecrets []string, jarPattern string) ([]utils.File, error) {
	var err error
	files := make([]utils.File, 0)
	keyChain, err := utils.NewKeyChain(ctx, cli, namespace, serviceAccount, imagePullSecrets)
	if err != nil {
		return nil, fmt.Errorf("failed to read keyChain for namespace: %s, serviceAccount: %s, imagePullSecrets: %s, %v", namespace, serviceAccount, imagePullSecrets, err)
	}
	image, err := utils.ReadImage(imageRef, remote.WithAuthFromKeychain(keyChain), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to read image: %s, %v\n", imageRef, err)
	}
	jarExpr := regexp.MustCompile(jarPattern)
	jar, err := utils.ReadFile(image, jarExpr)
	if err != nil {
		return nil, fmt.Errorf("failed to read project jar from image: %s, %v", imageRef, err)
	}
	if jar == nil {
		klog.V(log.D).Infof("No jar file matching the pattern: %s was found in image: %s", jarPattern, imageRef)
		return nil, nil
	}
	workflowFiles, err := workflowproj.ReadWorkflowFilesFromJar(jar.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow files from project jar: %s, %v", jar.Name, err)
	}
	fmt.Printf("Workflow files found in image: %d %s\n", len(files), imageRef)
	for workflowName, workflowContent := range workflowFiles {
		files = append(files, utils.File{
			Name:    workflowName,
			Content: workflowContent,
		})
		fmt.Printf("Workflow File: %s\n\n%s\n", workflowName, string(workflowContent))
	}
	return files, nil
}

func ParseWorkflowFiles(workflowFiles []utils.File) ([]cncfmodel.Workflow, error) {
	workflows := make([]cncfmodel.Workflow, 0)
	var workflow *cncfmodel.Workflow
	var err error
	for _, workflowFile := range workflowFiles {
		workflow = &cncfmodel.Workflow{}
		if workflowproj.IsJsonWorkflow(workflowFile.Name) {
			err = json.Unmarshal(workflowFile.Content, workflow)
		} else {
			err = yaml.Unmarshal(workflowFile.Content, workflow)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse cncf workflow: %s, %v", workflowFile.Name, err)
		}
		workflows = append(workflows, *workflow)
	}
	return workflows, nil
}
