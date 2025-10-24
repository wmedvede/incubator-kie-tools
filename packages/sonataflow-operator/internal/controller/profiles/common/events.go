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

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/constants"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/properties"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/workflowdef"
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
