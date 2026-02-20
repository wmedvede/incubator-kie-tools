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

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
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
	klog.V(log.D).Infof("ensure workflow 1\n")
	if requirePDB(workflow) {
		klog.V(log.D).Infof("ensure workflow 2\n")

		if workflow.Spec.PodTemplate.PodDisruptionBudget != nil {
			klog.V(log.D).Infof("HAY CHICHA PAPA: %s\n", workflow.Spec.PodTemplate.PodDisruptionBudget.MinAvailable)
		}

		pdb, _, err := h.podDisruptionBudget.Ensure(ctx, workflow, func(object client.Object) controllerutil.MutateFn {
			klog.V(log.D).Infof("Creando la mutadora sin nada papa 1\n")
			return func() error {
				klog.V(log.D).Infof("Mutando los mono papa 1\n")
				podDisruptionBudget := object.(*policyv1.PodDisruptionBudget)
				podDisruptionBudget.Spec.MinAvailable = workflow.Spec.PodTemplate.PodDisruptionBudget.MinAvailable
				podDisruptionBudget.Spec.MaxUnavailable = workflow.Spec.PodTemplate.PodDisruptionBudget.MaxUnavailable
				klog.V(log.D).Infof("Mutando los mono papa 2\n")
				return nil
			}
		})
		klog.V(log.D).Infof("ensure workflow 3\n")
		return pdb, err
	} else {
		klog.V(log.D).Infof("ensure workflow 4\n")
		pdb, err := findPDB(ctx, h.stateSupport.C, workflow.Namespace, workflow.Name)
		klog.V(log.D).Infof("ensure workflow 5\n")
		if err != nil {
			return nil, err
		}
		if pdb != nil {
			klog.V(log.D).Infof("ensure workflow 6\n")
			err = h.stateSupport.C.Delete(ctx, pdb)
			klog.V(log.D).Infof("ensure workflow 7\n")
		}
		return nil, err
	}
}

func requirePDB(workflow *operatorapi.SonataFlow) bool {
	return workflow.Spec.PodTemplate.PodDisruptionBudget != nil && workflow.Spec.PodTemplate.Replicas != nil && *workflow.Spec.PodTemplate.Replicas > 1
}

func findPDB(ctx context.Context, c client.Client, namespace string, name string) (*policyv1.PodDisruptionBudget, error) {
	klog.V(log.D).Infof("Querying PodDisruptionBudget %s/%s.", namespace, name)
	pdb := &policyv1.PodDisruptionBudget{}
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pdb)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return pdb, nil
}
