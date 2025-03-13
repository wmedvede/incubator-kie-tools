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

package common

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"

	"k8s.io/client-go/rest"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/discovery"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform/services"

	"k8s.io/client-go/tools/record"

	klog "k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
)

// StateSupport is the shared structure with common accessors used throughout the whole reconciliation profiles
type StateSupport struct {
	C        client.Client
	Cfg      *rest.Config
	Catalog  discovery.ServiceCatalog
	Recorder record.EventRecorder
}

// PerformStatusUpdate updates the SonataFlow Status conditions
func (s *StateSupport) PerformStatusUpdate(ctx context.Context, workflow *operatorapi.SonataFlow) (bool, error) {
	var err error
	pl, err := platform.GetActivePlatform(ctx, s.C, workflow.Namespace, true)
	if err != nil {
		return false, err
	}
	workflow.Status.ObservedGeneration = workflow.Generation
	workflow.Status.FlowCRC, err = utils.Crc32Checksum(workflow.Spec.Flow)
	if err != nil {
		return false, err
	}
	services.SetServiceUrlsInWorkflowStatus(pl, workflow)
	if workflow.Status.Platform == nil {
		workflow.Status.Platform = &operatorapi.SonataFlowPlatformRef{}
	}
	workflow.Status.Platform.Name = pl.Name
	workflow.Status.Platform.Namespace = pl.Namespace

	fmt.Printf("%s, do s.C.Status().Update() in PerformStatusUpdate on workflow: %s, resourceVersion: %s \n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())
	if err = s.C.Status().Update(ctx, workflow); err != nil {
		klog.V(log.E).ErrorS(err, "Failed to update Workflow status")
		return false, err
	}
	fmt.Printf("%s, after do s.C.Status().Update() in PerformStatusUpdate on workflow: %s, resourceVersion after update: %s \n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())

	return true, err
}

// Reconciler is the base structure used by every reconciliation profile.
// Use NewReconciler to build a new reference.
type Reconciler struct {
	*StateSupport
	reconciliationStateMachine *ReconciliationStateMachine
	objects                    []client.Object
}

func NewReconciler(support *StateSupport, stateMachine *ReconciliationStateMachine) Reconciler {
	return Reconciler{
		StateSupport:               support,
		reconciliationStateMachine: stateMachine,
	}
}

// Reconcile does the actual reconciliation algorithm based on a set of ReconciliationState
func (b *Reconciler) Reconcile(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, error) {
	workflow.Status.Manager().InitializeConditions()
	result, objects, err := b.reconciliationStateMachine.do(ctx, workflow)
	if err != nil {
		//TODO WM remove comment.
		//There are execution paths where we reach this point with both non-zero time result, and err.
		//This causes the kuberntes controller to print a warning.
		//E0312 13:12:26.579795       1 reconciler.go:75] "Failed to update Workflow status" err="Operation cannot be fulfilled on sonataflows.sonataflow.org \"callbackstatetimeouts\": the object has been modified; please apply your changes to the latest version and try again"
		//I0312 13:12:26.579931       1 controller.go:314] "msg"="Warning: Reconciler returned both a non-zero result and a non-nil error. The result will always be ignored if the error is non-nil and the non-nil error causes reqeueuing with exponential backoff. For more details, see: https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile#Reconciler" "SonataFlow"={"name":"callbackstatetimeouts","namespace":"case5-kn-eventing-workflows"} "controller"="sonataflow" "controllerGroup"="sonataflow.org" "controllerKind"="SonataFlow" "logger"="sonataflow-manager" "name"="callbackstatetimeouts" "namespace"="case5-kn-eventing-workflows" "reconcileID"="e9130872-4f05-4b87-af61-12d1fa149507"
		//E0312 13:12:26.579954       1 controller.go:316] "msg"="Reconciler error" "error"="Operation cannot be fulfilled on sonataflows.sonataflow.org \"callbackstatetimeouts\": the object has been modified; please apply your changes to the latest version and try again" "SonataFlow"={"name":"callbackstatetimeouts","namespace":"case5-kn-eventing-workflows"} "controller"="sonataflow" "controllerGroup"="sonataflow.org" "controllerKind"="SonataFlow" "logger"="sonataflow-manager" "name"="callbackstatetimeouts" "namespace"="case5-kn-eventing-workflows" "reconcileID"="e9130872-4f05-4b87-af61-12d1fa149507"

		return result, err
	}
	b.objects = objects
	klog.V(log.I).InfoS("Returning from reconciliation", "Result", result)

	return result, err
}

// NewReconciliationStateMachine builder for the ReconciliationStateMachine
func NewReconciliationStateMachine(states ...profiles.ReconciliationState) *ReconciliationStateMachine {
	return &ReconciliationStateMachine{
		states: states,
	}
}

// ReconciliationStateMachine implements (sort of) the command pattern and delegate to a chain of ReconciliationState
// the actual task to reconcile in a given workflow condition
//
// TODO: implement state transition, so based on a given condition we do the status update which actively transition the object state
type ReconciliationStateMachine struct {
	states []profiles.ReconciliationState
}

func (r *ReconciliationStateMachine) do(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, []client.Object, error) {
	for _, h := range r.states {
		if h.CanReconcile(workflow) {
			klog.V(log.I).InfoS("Found a condition to reconcile.", "Conditions", workflow.Status.Conditions)
			result, objs, err := h.Do(ctx, workflow)
			if err != nil {
				return result, objs, err
			}
			if err = h.PostReconcile(ctx, workflow); err != nil {
				klog.V(log.E).ErrorS(err, "Error in Post Reconcile actions.", "Workflow", workflow.Name, "Conditions", workflow.Status.Conditions)
			}
			return result, objs, err
		}
	}
	return ctrl.Result{}, nil, fmt.Errorf("the workflow %s in the namespace %s is in an unknown state condition. Can't reconcilie. Status is: %v", workflow.Name, workflow.Namespace, workflow.Status)
}
