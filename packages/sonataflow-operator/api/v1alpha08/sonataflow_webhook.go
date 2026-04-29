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

package v1alpha08

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
//var sonataflowlog = logf.Log.WithName("sonataflow-resource")

const SonataFlowKind = "SonataFlow"

func (r *SonataFlow) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func SetupSonataFlowWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&SonataFlow{}).
		WithValidator(&SonataFlowCustomValidator{}).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-sonataflow-org-v1alpha08-sonataflow,mutating=false,failurePolicy=fail,sideEffects=None,groups=sonataflow.org,resources=sonataflows,verbs=create;update,versions=v1alpha08,name=vsonataflow.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &SonataFlowCustomValidator{}

type SonataFlowCustomValidator struct{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *SonataFlowCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	fmt.Printf("SonataFlowCustomValidator.ValidateCreate\n")
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("expected admission.Request in ctx: %w", err)
	}
	if req.Kind.Kind != SonataFlowKind {
		return nil, fmt.Errorf("expected Kind %s got %s", SonataFlowKind, req.Kind.Kind)
	}

	workflow := obj.(*SonataFlow)
	fmt.Printf("SonataFlowCustomValidator.ValidateCreate for workflow %s/%s\n", workflow.Namespace, workflow.Name)

	if workflow.Name == "hello-fail" {
		return nil, fmt.Errorf("the workflow %s, is not valid", workflow.Name)
	}
	if workflow.Name == "hello-warning" {
		return []string{"w1: the workflow hello-warning", "w2: has some missing values", "w3: please check"}, nil
	}
	fmt.Printf("SonataFlowCustomValidator.ValidateCreate, excelent!, the workflow %s/%s is valid for creation!\n", workflow.Namespace, workflow.Name)
	return nil, nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *SonataFlowCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	fmt.Printf("SonataFlowCustomValidator.ValidateUpdate\n")
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("expected admission.Request in ctx: %w", err)
	}
	if req.Kind.Kind != SonataFlowKind {
		return nil, fmt.Errorf("expected Kind %s got %s", SonataFlowKind, req.Kind.Kind)
	}

	oldWorkflow := oldObj.(*SonataFlow)
	newWorkflow := newObj.(*SonataFlow)

	fmt.Printf("SonataFlowCustomValidator.ValidateUpdate for oldWorkflow %s/%s, with replicas: %d, newWorkflow %s/%s, with replicas: %d, \n", oldWorkflow.Namespace, oldWorkflow.Name, oldWorkflow.Spec.PodTemplate.Replicas, newWorkflow.Namespace, newWorkflow.Name, newWorkflow.Spec.PodTemplate.Replicas)

	if oldWorkflow.Name == "hello-fail-update" {
		return nil, fmt.Errorf("the workflow %s, is not valid for updating", newWorkflow.Name)
	}

	fmt.Printf("SonataFlowCustomValidator.ValidateUpdate, excelent!, the workflow %s/%s is valid for updating!\n", newWorkflow.Namespace, newWorkflow.Name)
	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *SonataFlowCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	fmt.Printf("SonataFlowCustomValidator.ValidateDelete\n")
	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("expected admission.Request in ctx: %w", err)
	}
	if req.Kind.Kind != SonataFlowKind {
		return nil, fmt.Errorf("expected Kind %s got %s", SonataFlowKind, req.Kind.Kind)
	}
	workflow := obj.(*SonataFlow)
	fmt.Printf("SonataFlowCustomValidator.ValidateDelete, excelent!, the workflow %s/%s is valid for deleting!\n", workflow.Namespace, workflow.Name)
	return nil, nil
}
