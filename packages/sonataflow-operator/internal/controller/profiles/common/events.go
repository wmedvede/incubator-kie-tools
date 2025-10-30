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

	"k8s.io/klog/v2"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/constants"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/properties"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/workflowdef"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"
)

func SendWorkFlowDefinitionAndSubFlowsAvailabilityEvent(workflow *operatorapi.SonataFlow, eventTargetUrl string, available bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), constants.EventDeliveryTimeout)
	defer cancel()
	evt := workflowdef.NewWorkflowDefinitionAvailabilityEvent(workflow, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrl(workflow), available)
	if err := utils.SendCloudEventWithContext(evt, ctx, eventTargetUrl); err != nil {
		return fmt.Errorf("failed to send workflow definition event: %v", err)
	}
	subFlowIds := make(map[string]string)
	for _, subFlow := range workflowdef.FindWorkflowRefs(workflow) {
		// TODO we need the version, must be necessary taken from the respective workflow definitions
		if _, ok := subFlowIds[subFlow.WorkflowID]; !ok {
			subFlowIds[subFlow.WorkflowID] = subFlow.WorkflowID
			subFlowEvt := workflowdef.NewSubFlowAvailabilityEvent(subFlow.WorkflowID, "1.0", workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrlWithNameAndNamespace(subFlow.WorkflowID, workflow.Namespace), available)
			if err := utils.SendCloudEventWithContext(subFlowEvt, ctx, eventTargetUrl); err != nil {
				return fmt.Errorf("failed to send subflow definition event: %v", err)
			}
		}
	}
	return nil
}

func GetOperatorNamespace() string {
	return "sonataflow-operator-system"
}

func GetOperatorServiceAccount() string {
	return "sonataflow-operator-controller-manager"
}

func GetOperatorPullSecrets() []string {
	return []string{"external-pull-secret"}
}

func SendWorkFlowAndSubFlowsDefinitionAvailabilityEvents(workflow *operatorapi.SonataFlow, eventTargetUrl string, available bool) error {
	imageRef := workflow.Spec.PodTemplate.Container.Image
	// Get the imagePullSecrets and the serviceAccountName the WF instead?
	// imagePullSecrets = workflow.Spec.PodTemplate.ImagePullSecrets        //If not set, OpenShift configures one like:   imagePullSecrets:  - name: default-dockercfg-2b5zb
	// we should probably get it from the POD in these cases.
	// serviceAccountName := workflow.Spec.PodTemplate.ServiceAccountName   //If not set, the value is default
	var ctx context.Context
	var cancel context.CancelFunc

	ctx, cancel = context.WithTimeout(context.Background(), constants.EventDeliveryTimeout)
	evt := workflowdef.NewWorkflowDefinitionAvailabilityEvent(workflow, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrl(workflow), available)
	if err := utils.SendCloudEventWithContext(evt, ctx, eventTargetUrl); err != nil {
		cancel()
		return fmt.Errorf("failed to send workflow definition event: %v", err)
	}
	cancel()
	subFlows := make(map[string]string)
	for _, subFlowRef := range workflowdef.FindWorkflowRefs(workflow) {
		if _, ok := subFlows[subFlowRef.WorkflowID]; !ok {
			subFlows[subFlowRef.WorkflowID] = subFlowRef.WorkflowID
		}
	}
	if len(subFlows) > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), constants.ImageReadTimeout)
		files, err := ReadWorkflowFilesFromImage(ctx, utils.GetKubernetesClient(), imageRef, GetOperatorNamespace(), GetOperatorServiceAccount(), GetOperatorPullSecrets(), ServerlessWorkflowProjectJarPattern)
		if err != nil {
			cancel()
			return fmt.Errorf("failed to read workflow files from image: %s, %w", imageRef, err)
		}
		cancel()
		cncfWorkflows, err := ParseWorkflowFiles(files)
		if err != nil {
			return fmt.Errorf("failed to parse workflow files extracted from image: %s, %v", imageRef, err)
		}
		for _, cncfWorkflow := range cncfWorkflows {
			if _, ok := subFlows[cncfWorkflow.ID]; !ok {
				klog.V(log.I).Infof("workflow: %s found in image: %s is not referred as subflow by the main workflow: %s, definition availability event will still be sent.", cncfWorkflow.ID, imageRef, workflow.Name)
			} else {
				delete(subFlows, cncfWorkflow.ID)
			}
			subFlowEvt := workflowdef.NewSubFlowAvailabilityEvent(cncfWorkflow.ID, cncfWorkflow.Version, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrlWithNameAndNamespace(cncfWorkflow.ID, workflow.Namespace), available)
			ctx, cancel = context.WithTimeout(context.Background(), constants.EventDeliveryTimeout)
			if err := utils.SendCloudEventWithContext(subFlowEvt, ctx, eventTargetUrl); err != nil {
				cancel()
				return fmt.Errorf("failed to send subflow definition event: %v", err)
			}
			cancel()
		}
		for _, subFlowRef := range subFlows {
			// should never happen, image building should have failed before reaching this case.
			klog.V(log.W).Infof("workflow: %s is referred as subflow by the main workflow: %s but is not found in image: %s", subFlowRef, workflow.Name, imageRef)
		}
	}
	return nil
}
