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

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/properties"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/workflowdef"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/preview"

	"github.com/google/uuid"

	"sigs.k8s.io/controller-runtime/pkg/predicate"

	sourcesv1 "knative.dev/eventing/pkg/apis/sources/v1"
	servingv1 "knative.dev/serving/pkg/apis/serving/v1"

	"k8s.io/klog/v2"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/knative"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/monitoring"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/metadata"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/constants"
	profiles "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/factory"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	prometheus "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform"
)

// SonataFlowReconciler reconciles a SonataFlow object
type SonataFlowReconciler struct {
	Client    client.Client
	Scheme    *runtime.Scheme
	Config    *rest.Config
	Recorder  record.EventRecorder
	RequestId string
}

//+kubebuilder:rbac:groups=sonataflow.org,resources=sonataflows,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sonataflow.org,resources=sonataflows/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sonataflow.org,resources=sonataflows/finalizers,verbs=update
//+kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups="serving.knative.dev",resources=revisions,verbs=list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the SonataFlow object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *SonataFlowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// Make sure the operator is allowed to act on namespace
	if ok, err := platform.IsOperatorAllowedOnNamespace(ctx, r.Client, req.Namespace); err != nil {
		return reconcile.Result{}, err
	} else if !ok {
		klog.V(log.I).InfoS("Ignoring request because the operator hasn't got the permissions to work on namespace", "namespace", req.Namespace)
		return reconcile.Result{}, nil
	}

	// Fetch the Workflow instance
	workflow := &operatorapi.SonataFlow{}
	err := r.Client.Get(ctx, req.NamespacedName, workflow)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.V(log.E).ErrorS(err, "Failed to get SonataFlow")
		return ctrl.Result{}, err
	}

	reconcileId := uuid.NewString()
	fmt.Printf("%s ---  %s - XXX START: Sonataflow controller Reconcile starting for workflow: %s, status:\n%s\n", time.Now().UTC().String(), reconcileId, workflow.Name, workflow.Status.String())

	r.setDefaults(workflow)
	// If the workflow is being deleted, execute all the associated finalizers
	if workflow.DeletionTimestamp != nil {
		return r.applyFinalizers(ctx, workflow)
	}

	// Only process resources assigned to the operator
	if !platform.IsOperatorHandlerConsideringLock(ctx, r.Client, req.Namespace, workflow) {
		klog.V(log.I).InfoS("Ignoring request because resource is not assigned to current operator")
		return reconcile.Result{}, nil
	}
	result, error := profiles.NewReconciler(r.Client, r.Config, r.Recorder, workflow).Reconcile(ctx, workflow)
	errorMessage := "Sin Error!"
	if error != nil {
		errorMessage = error.Error()
	}

	fmt.Printf("%s ---  %s - XXX END: Sonataflow controller Reconcile finalizing for workflow: %s, resourceVersion: %s, status:\n%s, error: %s\n", time.Now().UTC().String(), reconcileId, workflow.Name, workflow.GetResourceVersion(), workflow.Status.String(), errorMessage)

	return result, error
}

// applyFinalizers Manages the execution of the potential finalizers added to a workflow.
func (r *SonataFlowReconciler) applyFinalizers(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, error) {

	fmt.Printf("%s - sonataflow_controller.go.applyFinalizers - Start workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), workflow.Namespace, workflow.GetResourceVersion())

	if controllerutil.ContainsFinalizer(workflow, constants.TriggerFinalizer) {
		if err := r.cleanupTriggers(ctx, workflow); err != nil {
			return ctrl.Result{}, err
		}
	}
	if controllerutil.ContainsFinalizer(workflow, constants.WorkflowFinalizer) {
		if !workflow.Status.FinalizerSucceed && workflow.Status.FinalizerAttempts < constants.MaxWorkflowFinalizerAttempts {
			now := metav1.Now()
			workflow.Status.FinalizerAttempts = workflow.Status.FinalizerAttempts + 1
			workflow.Status.LastTimeFinalizerAttempt = &now

			fmt.Printf("%s - sonataflow_controller.go.applyFinalizers - before status update and programming preview.AsyncRunner.RunAsync workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), workflow.Namespace, workflow.GetResourceVersion())

			if err := r.Client.Status().Update(ctx, workflow); err != nil {
				return ctrl.Result{}, err
			}

			fmt.Printf("%s - sonataflow_controller.go.applyFinalizers - after status update and programming preview.AsyncRunner.RunAsync workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), workflow.Namespace, workflow.GetResourceVersion())

			preview.AsyncRunner.RunAsync(func() error {
				notifyWorkflowDeletion2(r.Client, workflow.Name, workflow.Namespace, workflow.GetResourceVersion())
				return nil
			})

			return ctrl.Result{RequeueAfter: constants.WorkflowFinalizerRetryInterval}, nil
		} else {
			fmt.Printf("%s - sonataflow_controller.go.applyFinalizers - before remove WorkflowFinalizer preview.AsyncRunner.RunAsync workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), workflow.Namespace, workflow.GetResourceVersion())
			controllerutil.RemoveFinalizer(workflow, constants.WorkflowFinalizer)
			if err := r.Client.Update(ctx, workflow); err != nil {
				return ctrl.Result{}, err
			}
			fmt.Printf("%s - sonataflow_controller.go.applyFinalizers - after remove WorkflowFinalizer preview.AsyncRunner.RunAsync workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), workflow.Namespace, workflow.GetResourceVersion())
		}
	}
	return ctrl.Result{}, nil
}

// TODO: move to webhook see https://github.com/apache/incubator-kie-tools/packages/sonataflow-operator/pull/239
func (r *SonataFlowReconciler) setDefaults(workflow *operatorapi.SonataFlow) {
	if workflow.Annotations == nil {
		workflow.Annotations = map[string]string{}
	}
	profile := metadata.GetProfileOrDefault(workflow.Annotations)
	workflow.Annotations[metadata.Profile] = string(profile)
	if profile == metadata.DevProfile {
		workflow.Spec.PodTemplate.DeploymentModel = operatorapi.KubernetesDeploymentModel
	}
}

func (r *SonataFlowReconciler) cleanupTriggers(ctx context.Context, workflow *operatorapi.SonataFlow) error {
	for _, triggerRef := range workflow.Status.Triggers {
		trigger := &eventingv1.Trigger{
			ObjectMeta: metav1.ObjectMeta{
				Name:      triggerRef.Name,
				Namespace: triggerRef.Namespace,
			},
		}
		if err := r.Client.Delete(ctx, trigger); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	controllerutil.RemoveFinalizer(workflow, constants.TriggerFinalizer)
	return r.Client.Update(ctx, workflow)
}

func (r *SonataFlowReconciler) workflowDeletion(ctx context.Context, workflow *operatorapi.SonataFlow) error {

	var err error
	var sfp *operatorapi.SonataFlowPlatform

	if sfp, err = common.GetDataIndexPlatform(context.Background(), r.Client, workflow); err != nil {
		return err
	}
	if sfp == nil {
		klog.V(log.I).Infof("No DataIndex containing platform was found for workflow: %s, namespace: %s, for un-deployment notification.", workflow.Name, workflow.Namespace)
	} else {
		evt := workflowdef.NewWorkflowDefinitionAvailabilityEvent(workflow, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrl(workflow), false)
		if err = common.SendWorkflowDefinitionEvent(ctx, workflow, sfp, evt); err != nil {
			return err
		}
	}

	return nil
}

const sendStatusUpdateGenericError = "An error was produced while sending workflow status update event."

func notifyWorkflowDeletion2(cli client.Client, wfName, wfNamespace string, wfResourceVersion string) {

	var err error
	var sfp *operatorapi.SonataFlowPlatform

	size := preview.AsyncRunner.Len()
	fmt.Printf("sonataflow_controller.go.notifyWorkflowDeletion2, Channel Len() = %d\n", size)
	if size == 0 {
		fmt.Printf("Channel has no elements! let's try to sleep")
		time.Sleep(time.Duration(time.Millisecond * 500))
	}
	fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2 - Start workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), wfName, wfResourceVersion)

	workflow := &operatorapi.SonataFlow{}
	if err = cli.Get(context.Background(), types.NamespacedName{Name: wfName, Namespace: wfNamespace}, workflow); err != nil {
		if errors.IsNotFound(err) {
			klog.V(log.I).Infof(sendStatusUpdateGenericError+" Workflow: %s, namespace: %s, was not found.", wfName, wfNamespace)
		} else {
			klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" It was not possible to read the workflow.", "workflow", "namespace", wfName, wfNamespace)
		}
		return
	}

	fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2 - workflow: %s, wfResourceVersion: %s, currentResourceVersion: %s, currentStatus: %s\n",
		time.Now().UTC().String(), wfName, wfResourceVersion, workflow.GetResourceVersion(), workflow.Status.String())

	workflow = workflow.DeepCopy()

	if sfp, err = common.GetDataIndexPlatform(context.Background(), cli, workflow); err != nil {
		klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" It was not possible to get the DataIndex containing platform.", "workflow", "namespace", workflow.Name, workflow.Namespace)
		return
	}
	if sfp == nil {
		klog.V(log.I).Infof("No DataIndex containing platform was found for workflow: %s, namespace: %s, to send the workflow definition status change event.",
			workflow.Name, workflow.Namespace)
	} else {
		evt := workflowdef.NewWorkflowDefinitionAvailabilityEvent(workflow, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrl(workflow), false)
		if err = common.SendWorkflowDefinitionEvent(context.Background(), workflow, sfp, evt); err != nil {
			fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2, Error sending wf event: %s\n", time.Now().UTC().String(), workflow.Name)

			workflow.Status.FinalizerSucceed = false
			klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" Even delivery failed", "workflow", "namespace", workflow.Name, workflow.Namespace)
		} else {
			workflow.Status.FinalizerSucceed = true
			fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2, before cli.Status().Update(context.Background(), workflow) on workflow: %s, resourceVersion: %s\n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())
			if err = cli.Status().Update(context.Background(), workflow); err != nil {
				fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2, PerformStatusUpdate ERROR on workflow: %s, %s\n", time.Now().UTC().String(), workflow.Name, err.Error())
				klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" Workflow status update failed.", "workflow", "namespace", workflow.Name, workflow.Namespace)
			} else {
				fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2, after cli.Status().Update(context.Background(), workflow) on workflow: %s, resourceVersion: %s\n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())

				fmt.Printf("%s - sonataflow_controller.go.notifyWorkflowDeletion2, PerformStatusUpdate OK on workflow: %s\n", time.Now().UTC().String(), workflow.Name)
			}
		}
	}
}

// Delete implements a handler for the Delete event.
func (r *SonataFlowReconciler) Delete(e event.DeleteEvent) error {
	return nil
}

func platformEnqueueRequestsFromMapFunc(c client.Client, p *operatorapi.SonataFlowPlatform) []reconcile.Request {
	var requests []reconcile.Request

	if p.Status.IsReady() {
		list := &operatorapi.SonataFlowList{}

		// Do global search in case of global operator (it may be using a global platform)
		var opts []client.ListOption
		if !platform.IsCurrentOperatorGlobal() {
			opts = append(opts, client.InNamespace(p.Namespace))
		}

		if err := c.List(context.Background(), list, opts...); err != nil {
			klog.V(log.E).ErrorS(err, "Failed to list workflows")
			return requests
		}

		for _, workflow := range list.Items {
			cond := workflow.Status.GetTopLevelCondition()
			if cond.IsFalse() && api.WaitingForPlatformReason == cond.Reason {
				klog.V(log.I).InfoS("Platform ready, wake-up workflow", "platform", p.Name, "workflow", workflow.Name)
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: workflow.Namespace,
						Name:      workflow.Name,
					},
				})
			}
		}
	}
	return requests
}

func buildEnqueueRequestsFromMapFunc(c client.Client, b *operatorapi.SonataFlowBuild) []reconcile.Request {
	var requests []reconcile.Request

	if b.Status.BuildPhase == operatorapi.BuildPhaseSucceeded {
		// Fetch the Workflow instance
		workflow := &operatorapi.SonataFlow{}
		namespacedName := types.NamespacedName{
			Namespace: workflow.Namespace,
			Name:      workflow.Name,
		}
		err := c.Get(context.Background(), namespacedName, workflow)
		if err != nil {
			if errors.IsNotFound(err) {
				return requests
			}
			klog.V(log.I).ErrorS(err, "Failed to get SonataFlow")
			return requests
		}

		if workflow.Status.IsBuildRunningOrUnknown() {
			klog.V(log.I).InfoS("Build %s ready, wake-up workflow: %s", b.Name, workflow.Name)
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: workflow.Namespace,
					Name:      workflow.Name,
				},
			})
		}

	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonataFlowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&operatorapi.SonataFlow{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldGeneration := e.ObjectOld.GetGeneration()
				newGeneration := e.ObjectNew.GetGeneration()
				// Generation is only updated on spec changes (also on deletion), not upon metadata or status changes.
				// Filter out events where the generation hasn't changed to avoid being triggered by status updates.
				return oldGeneration != newGeneration
			},
		}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&operatorapi.SonataFlowBuild{}).
		Watches(&operatorapi.SonataFlowPlatform{}, handler.EnqueueRequestsFromMapFunc(func(c context.Context, a client.Object) []reconcile.Request {
			plat, ok := a.(*operatorapi.SonataFlowPlatform)
			if !ok {
				klog.V(log.E).InfoS("Failed to retrieve workflow list. Type assertion failed", "assertion", a)
				return []reconcile.Request{}
			}
			return platformEnqueueRequestsFromMapFunc(mgr.GetClient(), plat)
		})).
		Watches(&operatorapi.SonataFlowBuild{}, handler.EnqueueRequestsFromMapFunc(func(c context.Context, a client.Object) []reconcile.Request {
			build, ok := a.(*operatorapi.SonataFlowBuild)
			if !ok {
				klog.V(log.I).ErrorS(fmt.Errorf("type assertion failed: %v", a), "Failed to retrieve workflow list")
				return []reconcile.Request{}
			}
			return buildEnqueueRequestsFromMapFunc(mgr.GetClient(), build)
		}))

	knativeAvail, err := knative.GetKnativeAvailability(mgr.GetConfig())
	if err != nil {
		return err
	}
	if knativeAvail.Serving {
		builder = builder.Owns(&servingv1.Service{})
	}
	if knativeAvail.Eventing {
		builder = builder.Owns(&eventingv1.Trigger{}).
			Owns(&sourcesv1.SinkBinding{}).
			Watches(&eventingv1.Trigger{}, handler.EnqueueRequestsFromMapFunc(knative.MapTriggerToPlatformRequests))
	}
	promAvail, err := monitoring.GetPrometheusAvailability(mgr.GetConfig())
	if err != nil {
		return err
	}
	if promAvail {
		builder = builder.Owns(&prometheus.ServiceMonitor{})
	}

	//main2()
	fmt.Printf("XXXXXXXXXXXXX Sonatafow Controller inicialization has finished\n")
	preview.AsyncRunner.Start()
	return builder.Complete(r)
}

func main2() {
	//var wg sync.WaitGroup
	//wg.Add(1)

	go func(message string) {
		count := 1
		for {
			fmt.Printf("%s: %d\n", message, count)
			count++
			time.Sleep(2 * time.Second)
		}
	}("Sending cloud event")
	//wg.Wait()
}
