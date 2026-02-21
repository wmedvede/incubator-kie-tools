/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package kubernetes

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
)

// FindHPAForDeployment returns the HorizontalPodAutoscaler targeting a deployment in a given namespace, or nil if it
// doesn't exist.
// Note: By k8s definition, the HorizontalPodAutoscaler must belong to the same namespace as the managed deployment.
func FindHPAForDeployment(ctx context.Context, c client.Client, namespace string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return findHPAForTarget(ctx, c, namespace, "appls/v1", "Deployment", name)
}

// FindHPAForWorkflow returns the HorizontalPodAutoscaler targeting a workflow in a given namespace, or nil if it
// doesn't exist.
// Note: By k8s definition, the HorizontalPodAutoscaler must belong to the same namespace as the managed workflow.
func FindHPAForWorkflow(ctx context.Context, c client.Client, namespace string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	return findHPAForTarget(ctx, c, namespace, "sonataflow.org/v1alpha08", "SonataFlow", name)
}

func findHPAForTarget(ctx context.Context, c client.Client, namespace string, apiVersion string, kind string, name string) (*autoscalingv2.HorizontalPodAutoscaler, error) {
	klog.V(log.D).Infof("Querying HorizontalPodAutoscalers in namespace: %s.", namespace)
	var hpaList autoscalingv2.HorizontalPodAutoscalerList
	if err := c.List(ctx, &hpaList, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	klog.V(log.D).Infof("Total number of returned HorizontalPodAutoscalers is: %d.", len(hpaList.Items))
	for _, hpa := range hpaList.Items {
		ref := hpa.Spec.ScaleTargetRef
		klog.V(log.D).Infof("Checking if HorizontalPodAutoscaler name: %s, ref.APIVersion: %s, ref.Kind: %s, ref.Name: %s, targets apiVersion: %s, kind: %s, name: %s.", hpa.Name, ref.APIVersion, ref.Kind, ref.Name, apiVersion, kind, name)
		if ref.Kind == kind && ref.Name == name && ref.APIVersion == apiVersion {
			klog.V(log.D).Infof("HorizontalPodAutoscaler name: %s targets apiVersion: %s, kind: %s, name: %s.", hpa.Name, apiVersion, kind, name)
			return &hpa, nil
		}
	}
	klog.V(log.D).Infof("No HorizontalPodAutoscaler targets apiVersion: %s, kind: %s, name: %s.", apiVersion, kind, name)
	return nil, nil
}

// HPAIsActive returns true if the HorizontalPodAutoscaler is active.
func HPAIsActive(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	klog.V(log.D).Infof("Checking if HorizontalPodAutoscaler is Active.")
	for _, cond := range hpa.Status.Conditions {
		klog.V(log.D).Infof("Checking Status condition type: %s, %s.", cond.Type, cond.Status)
		if cond.Type == autoscalingv2.ScalingActive {
			return cond.Status == v1.ConditionTrue
		}
	}
	return false
}

// HPAIsWorking returns true if the HorizontalPodAutoscaler has started to take care of the scaling for the
// corresponding target ref. At this point, our controllers must transfer the control to the HorizontalPodAutoscaler
// and let it manage the replicas.
func HPAIsWorking(hpa *autoscalingv2.HorizontalPodAutoscaler) bool {
	return HPAIsActive(hpa) || hpa.Status.DesiredReplicas > 0
}
