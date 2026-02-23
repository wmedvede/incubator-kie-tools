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

package preview

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"

	policyv1 "k8s.io/api/policy/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils/kubernetes"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common"
)

type podDisruptionBudgetHandler struct {
	stateSupport        *common.StateSupport
	podDisruptionBudget common.ObjectEnsurer
}

type PodDisruptionBudgetHandler interface {
	Ensure(ctx context.Context, workflow *operatorapi.SonataFlow) (client.Object, error)
}

func NewPodDisruptionBudgetHandler(support *common.StateSupport) PodDisruptionBudgetHandler {
	return podDisruptionBudgetHandler{
		stateSupport:        support,
		podDisruptionBudget: common.NewObjectEnsurer(support.C, common.PodDisruptionBudgetCreator),
	}
}

func (h podDisruptionBudgetHandler) Ensure(ctx context.Context, workflow *operatorapi.SonataFlow) (client.Object, error) {
	createOrUpdate := false
	if workflow.Spec.PodTemplate.DeploymentModel == operatorapi.KnativeDeploymentModel {
		return nil, nil
	}
	if workflow.Spec.PodTemplate.PodDisruptionBudget != nil {
		klog.V(log.D).Infof("Finding HPA for workflow: %s/%s", workflow.Namespace, workflow.Name)
		hpa, err := kubernetes.FindHPAForWorkflow(ctx, h.stateSupport.C, workflow.Namespace, workflow.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to find a potential HorizontalPodAutoscaler for workflow: %s/%s: %v", workflow.Namespace, workflow.Name, err)
		}
		if hpa != nil {
			klog.V(log.D).Infof("HPA %s/%s was found for workflow %s/%s", hpa.Namespace, hpa.Name, workflow.Namespace, workflow.Name)
			// The HPA determines the replicas. Be sure that the workflow can't be later downscaled to a number of replicas that blocks a drain.
			createOrUpdate = hpa.Spec.MinReplicas != nil && *hpa.Spec.MinReplicas > int32(1)
			klog.V(log.D).Infof("HPA %s/%s createOrUpdate: %t", hpa.Namespace, hpa.Name, createOrUpdate)
		} else {
			// The replicas are determined from the workflow spec. Be sure that the number of replicas don't block a drain.
			createOrUpdate = workflow.Spec.PodTemplate.Replicas != nil && *workflow.Spec.PodTemplate.Replicas > int32(1)
		}
	}

	if createOrUpdate {
		pdb, _, err := h.podDisruptionBudget.Ensure(ctx, workflow, func(object client.Object) controllerutil.MutateFn {
			return func() error {
				currentPdb := object.(*policyv1.PodDisruptionBudget)
				currentPdb.Spec.MinAvailable = workflow.Spec.PodTemplate.PodDisruptionBudget.MinAvailable
				currentPdb.Spec.MaxUnavailable = workflow.Spec.PodTemplate.PodDisruptionBudget.MaxUnavailable
				return nil
			}
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create or update PodDiscruptionBudget for workflow's deployment %s/%s: %v", workflow.Namespace, workflow.Name, err)
		}
		return pdb, nil
	} else {
		// Remove a potential previously created PDB if any.
		pdb, err := kubernetes.FindPDB(ctx, h.stateSupport.C, workflow.Namespace, workflow.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to find a potential PodDiscruptionBudget for workflow's deployment %s/%s: %v", workflow.Namespace, workflow.Name, err)
		}
		if pdb != nil {
			err = h.stateSupport.C.Delete(ctx, pdb)
			if err != nil {
				return nil, fmt.Errorf("failed to delete PodDiscruptionBudget for workflow's deployment %s/%s: %v", workflow.Namespace, workflow.Name, err)
			}
		}
		return nil, nil
	}
}
