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
	"time"

	"k8s.io/client-go/util/retry"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/properties"

	"k8s.io/klog/v2"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/workflowdef"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/knative"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api"
	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/monitoring"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"
)

type DeploymentReconciler struct {
	*common.StateSupport
	ensurers *ObjectEnsurers
}

var AsyncRunner = NewDeploymentWorker()

type Runnable func() error

type DeploymentWorker struct {
	ch chan Runnable
}

func NewDeploymentWorker() DeploymentWorker {
	return DeploymentWorker{ch: make(chan Runnable, 100)}
}

func (w DeploymentWorker) Start() {
	go func(ch chan Runnable) {
		for {
			r, ok := <-ch
			if !ok {
				break
			} else {
				if err := r(); err != nil {
					fmt.Printf("An error was produced during Runnable excution: %s\n", err.Error())
				}
			}
		}
	}(w.ch)
}

func (w DeploymentWorker) RunAsync(r Runnable) {
	fmt.Printf("%s - DeploymentWorker, programming runnable\n", time.Now().UTC().String())
	w.ch <- r
}

func (w DeploymentWorker) Len() int {
	return len(w.ch)
}

func NewDeploymentReconciler(stateSupport *common.StateSupport, ensurer *ObjectEnsurers) *DeploymentReconciler {
	return &DeploymentReconciler{
		StateSupport: stateSupport,
		ensurers:     ensurer,
	}
}

func (d *DeploymentReconciler) Reconcile(ctx context.Context, workflow *operatorapi.SonataFlow) (reconcile.Result, []client.Object, error) {
	return d.reconcileWithImage(ctx, workflow, "")
}

func (d *DeploymentReconciler) reconcileWithImage(ctx context.Context, workflow *operatorapi.SonataFlow, image string) (reconcile.Result, []client.Object, error) {
	fmt.Printf("%s - XXX NewDeploymentReconciler.reconcileWithImage, resoruceVersion: %s\n", time.Now().UTC().String(), workflow.GetResourceVersion())
	// Checks if we need Knative installed and is not present.
	if requires, err := d.ensureKnativeServingRequired(workflow); requires || err != nil {
		return reconcile.Result{Requeue: false}, nil, err
	}

	previousStatus := workflow.Status
	// Ensure objects
	result, objs, err := d.ensureObjects(ctx, workflow, image)
	if err != nil || result.Requeue {
		return result, objs, err
	}

	// Follow deployment status
	result, err = common.DeploymentManager(d.C).SyncDeploymentStatus(ctx, workflow)
	if err != nil {
		return reconcile.Result{Requeue: false}, nil, err
	}

	d.updateLastTimeStatusNotified(workflow, previousStatus)

	fmt.Printf("deployment_handler.go.reconcileWithImage , PerformStatusUpdate on workflow: %s\n", workflow.Name)
	if _, err := d.PerformStatusUpdate(ctx, workflow); err != nil {
		fmt.Printf("deployment_handler.go.reconcileWithImage , error en el PerformStatusUpdate on workflow: %s, error: %s\n", workflow.Name, err.Error())

		return reconcile.Result{Requeue: false}, nil, err
	}

	d.notifyStatusUpdate(ctx, workflow)

	return result, objs, nil
}

// ensureKnativeServingRequired returns true if the SonataFlow instance requires Knative deployment and Knative Serving is not available.
func (d *DeploymentReconciler) ensureKnativeServingRequired(workflow *operatorapi.SonataFlow) (bool, error) {
	if workflow.IsKnativeDeployment() {
		avail, err := knative.GetKnativeAvailability(d.Cfg)
		if err != nil {
			return true, err
		}
		if !avail.Serving {
			d.Recorder.Eventf(workflow, v1.EventTypeWarning,
				"KnativeServingNotAvailable",
				"Knative Serving is not available in this cluster, can't deploy workflow. Please update the deployment model to %s",
				operatorapi.KubernetesDeploymentModel)
			return true, nil
		}
	}
	return false, nil
}

func (d *DeploymentReconciler) ensureObjects(ctx context.Context, workflow *operatorapi.SonataFlow, image string) (reconcile.Result, []client.Object, error) {
	pl, _ := platform.GetActivePlatform(ctx, d.C, workflow.Namespace, true)
	userPropsCM, _, err := d.ensurers.userPropsConfigMap.Ensure(ctx, workflow)
	if err != nil {
		fmt.Printf("deployment_handler.go.ensureObjects A , PerformStatusUpdate on workflow: %s\n", workflow.Name)
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.ExternalResourcesNotFoundReason, "Unable to retrieve the user properties config map")
		_, _ = d.PerformStatusUpdate(ctx, workflow)
		return reconcile.Result{}, nil, err
	}
	managedPropsCM, _, err := d.ensurers.managedPropsConfigMap.Ensure(ctx, workflow, pl,
		common.ManagedPropertiesMutateVisitor(ctx, d.StateSupport.Catalog, workflow, pl, userPropsCM.(*v1.ConfigMap)))
	if err != nil {
		fmt.Printf("deployment_handler.go.ensureObjects B , PerformStatusUpdate on workflow: %s\n", workflow.Name)
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.ExternalResourcesNotFoundReason, "Unable to retrieve the managed properties config map")
		_, _ = d.PerformStatusUpdate(ctx, workflow)
		return reconcile.Result{}, nil, err
	}

	deployment, deploymentOp, err :=
		d.ensurers.DeploymentByDeploymentModel(workflow).Ensure(ctx, workflow, pl,
			d.deploymentModelMutateVisitors(workflow, pl, image, userPropsCM.(*v1.ConfigMap), managedPropsCM.(*v1.ConfigMap))...)
	if err != nil {
		fmt.Printf("deployment_handler.go.ensureObjects C, PerformStatusUpdate on workflow: %s\n", workflow.Name)
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.DeploymentUnavailableReason, "Unable to perform the deploy due to ", err)
		_, _ = d.PerformStatusUpdate(ctx, workflow)
		return reconcile.Result{}, nil, err
	}

	service, _, err := d.ensurers.ServiceByDeploymentModel(workflow).Ensure(ctx, workflow, common.ServiceMutateVisitor(workflow))
	if err != nil {
		fmt.Printf("deployment_handler.go.ensureObjects D, PerformStatusUpdate on workflow: %s\n", workflow.Name)

		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.DeploymentUnavailableReason, "Unable to make the service available due to ", err)
		_, _ = d.PerformStatusUpdate(ctx, workflow)
		return reconcile.Result{}, nil, err
	}

	objs := []client.Object{deployment, managedPropsCM, service}
	eventingObjs, err := common.NewKnativeEventingHandler(d.StateSupport, pl).Ensure(ctx, workflow)
	if err != nil {
		return reconcile.Result{}, nil, err
	}
	objs = append(objs, eventingObjs...)

	serviceMonitor, err := d.ensureServiceMonitor(ctx, workflow, pl)
	if err != nil {
		return reconcile.Result{}, nil, err
	}
	if serviceMonitor != nil {
		objs = append(objs, serviceMonitor)
	}

	if deploymentOp == controllerutil.OperationResultCreated {
		fmt.Printf("deployment_handler.go.ensureObjects E, PerformStatusUpdate on workflow: %s\n", workflow.Name)

		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForDeploymentReason, "")
		if _, err := d.PerformStatusUpdate(ctx, workflow); err != nil {
			return reconcile.Result{}, nil, err
		}
		//TODO WM restore this to the constant
		return reconcile.Result{RequeueAfter: 30 * time.Second /*constants.RequeueAfterFollowDeployment*/, Requeue: true}, objs, nil
	}
	return reconcile.Result{}, objs, nil
}

func (d *DeploymentReconciler) ensureServiceMonitor(ctx context.Context, workflow *operatorapi.SonataFlow, pl *operatorapi.SonataFlowPlatform) (client.Object, error) {
	if monitoring.IsMonitoringEnabled(pl) {
		serviceMonitor, _, err := d.ensurers.ServiceMonitorByDeploymentModel(workflow).Ensure(ctx, workflow)
		return serviceMonitor, err
	}
	return nil, nil
}

func (d *DeploymentReconciler) deploymentModelMutateVisitors(
	workflow *operatorapi.SonataFlow,
	plf *operatorapi.SonataFlowPlatform,
	image string,
	userPropsCM *v1.ConfigMap,
	managedPropsCM *v1.ConfigMap) []common.MutateVisitor {

	if workflow.IsKnativeDeployment() {
		return []common.MutateVisitor{common.KServiceMutateVisitor(workflow, plf),
			common.ImageKServiceMutateVisitor(workflow, image),
			mountConfigMapsMutateVisitor(workflow, userPropsCM, managedPropsCM),
			common.RestoreKServiceVolumeAndVolumeMountMutateVisitor(),
		}
	}

	if utils.IsOpenShift() {
		return []common.MutateVisitor{common.DeploymentMutateVisitor(workflow, plf),
			mountConfigMapsMutateVisitor(workflow, userPropsCM, managedPropsCM),
			addOpenShiftImageTriggerDeploymentMutateVisitor(workflow, image),
			common.ImageDeploymentMutateVisitor(workflow, image),
			common.RestoreDeploymentVolumeAndVolumeMountMutateVisitor(),
			common.RolloutDeploymentIfCMChangedMutateVisitor(workflow, userPropsCM, managedPropsCM),
		}
	}
	return []common.MutateVisitor{common.DeploymentMutateVisitor(workflow, plf),
		common.ImageDeploymentMutateVisitor(workflow, image),
		mountConfigMapsMutateVisitor(workflow, userPropsCM, managedPropsCM),
		common.RestoreDeploymentVolumeAndVolumeMountMutateVisitor(),
		common.RolloutDeploymentIfCMChangedMutateVisitor(workflow, userPropsCM, managedPropsCM)}
}

func (d *DeploymentReconciler) updateLastTimeStatusNotified(workflow *operatorapi.SonataFlow,
	previousStatus operatorapi.SonataFlowStatus) {

	previousRunningCondition := previousStatus.GetCondition(api.RunningConditionType)
	currentRunningCondition := workflow.Status.GetCondition(api.RunningConditionType)

	if previousRunningCondition == nil {
		previousRunningCondition = currentRunningCondition
	}

	fmt.Printf("DeploymentHandler.updateLastTimeStatusNotified,\n previousStatus: %s\n, currentStatus: %s\n, lastTimeStatusNotified: %v\n", previousStatus.String(), workflow.Status.String(), previousStatus.LastTimeStatusNotified)
	changed := false
	//TODO WM, aca tengo que considerar el caso donde
	//el controller esta reiniciando y tal vez me conviene mandar el evento de update.
	//podria pasar que no tengo cambio destatus en el sentido del deployment, pero por seguridad
	//me puede interesar de todas formas mandar el evento confirmando el estado
	// para eso puedo usar la hora de inicio del controller. Y si tengo que
	// la hora de inicio del controller es posterior a la hora del ultimo status change
	// envio el evento. si es redundante, mala suerte, un evento mas no molesta.
	// Cuidado, porque el controller se reinicia tambien cuando hacemos el cambio de version del operator.
	// tendria que probar como se comporta cuando paso de la 1.35.0 a la 1.36.0....
	// porque en paralelo, cuando hacemos el cambio de version, normalmente se nos reinician los servicios tambien...
	// tengo que probarlo.
	if previousRunningCondition.Status != currentRunningCondition.Status {
		changed = true
		workflow.Status.LastTimeStatusNotified = nil
	}
	fmt.Printf("DeploymentHandler.updateLastTimeStatusNotified, statusChanged: %t, available: %t\n", changed, currentRunningCondition.IsTrue())
}

func (d *DeploymentReconciler) notifyStatusUpdate(ctx context.Context, workflow *operatorapi.SonataFlow) {
	fmt.Printf("DeploymentHandler.notifyStatusUpdate, workflow: %s, lastTimeStatusNotified: %v\n", workflow.Name, workflow.Status.LastTimeStatusNotified)
	if workflow.Status.LastTimeStatusNotified == nil {
		fmt.Printf("DeploymentHandler.notifyStatusUpdate, program the RunAsync for resourceVersion: %s\n", workflow.GetResourceVersion())
		AsyncRunner.RunAsync(func() error {
			available := workflow.Status.GetCondition(api.RunningConditionType).IsTrue()
			fmt.Printf("%s - Ejecutando el AsyncRunner: con workflow: %s, available al llamar: %t\n", time.Now().UTC().String(), workflow.Name, available)
			sendStatusUpdateEvent(d.C, workflow.Name, workflow.Namespace, workflow.GetResourceVersion())
			return nil
		})
	}

}

func (d *DeploymentReconciler) notifyStatusUpdateOLD(ctx context.Context, workflow *operatorapi.SonataFlow,
	previousStatus operatorapi.SonataFlowStatus) error {

	previousRunningCondition := previousStatus.GetCondition(api.RunningConditionType)
	currentRunningCondition := workflow.Status.GetCondition(api.RunningConditionType)

	fmt.Printf("DeploymentHandler.notifyStatusUpdate,\n previousStatus: %s\n, currentStatus: %s\n", previousStatus.String(), workflow.Status.String())

	if previousRunningCondition == nil {
		previousRunningCondition = currentRunningCondition
	}

	fmt.Printf("previousRunningCondition: %s\n, currentRunningCondition: %s, lastTimeStatusNotified: %v\n",
		previousRunningCondition.String(), currentRunningCondition.String(), previousStatus.LastTimeStatusNotified)

	if previousRunningCondition.Status != currentRunningCondition.Status || previousStatus.LastTimeStatusNotified == nil {
		available := currentRunningCondition.IsTrue()

		fmt.Printf("%s - Status condition changed: %s, available: %t\n", time.Now().UTC().String(), workflow.Name, currentRunningCondition.IsTrue())
		fmt.Printf("We must add the RunAsync\n")

		AsyncRunner.RunAsync(func() error {
			fmt.Printf("%s - Ejecutando el AsyncRunner: con workflow: %s, available: %t\n", time.Now().UTC().String(), workflow.Name, available)
			sendStatusUpdateEvent(d.C, workflow.Name, workflow.Namespace, workflow.GetResourceVersion())
			return nil
		})
	} else {
		fmt.Printf("%s - Status condition NOT changed: %s, available: %t\n", time.Now().UTC().String(), workflow.Name, currentRunningCondition.IsTrue())
	}
	return nil
}

const sendStatusUpdateGenericError = "An error was produced while sending workflow status update event."

/*
func useRetry() {

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the resource here; you need to refetch it on every try, since
		// if you got a conflict on the last update attempt then you need to get
		// the current version before making your own changes.
		pod, err := c.Pods("mynamespace").Get(name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		// Make whatever updates to the resource are needed
		pod.Status.Phase = v1.PodFailed

		// Try to update
		_, err = c.Pods("mynamespace").UpdateStatus(pod)
		// You have to return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	if err != nil {
		// May be conflict if max retries were hit, or may be something unrelated
		// like permissions or a network error
		return err
	}

}
*/

func sendStatusUpdateEvent(cli client.Client, wfName, wfNamespace string, wfResourceVersion string) {

	var err error
	var sfp *operatorapi.SonataFlowPlatform

	fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent - Start workflow: %s, wfResourceVersion: %s\n", time.Now().UTC().String(), wfName, wfResourceVersion)
	size := AsyncRunner.Len()
	fmt.Printf("deployment_handler.go.sendStatusUpdateEvent, Channel Len() = %d\n", size)

	retryNumber := 1

	retryErr := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		workflow := &operatorapi.SonataFlow{}
		if err = cli.Get(context.Background(), types.NamespacedName{Name: wfName, Namespace: wfNamespace}, workflow); err != nil {
			if errors.IsNotFound(err) {
				klog.V(log.I).Infof(sendStatusUpdateGenericError+" Workflow: %s, namespace: %s, was not found.", wfName, wfNamespace)
				return nil
			} else {
				klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" It was not possible to read the workflow.", "workflow", "namespace", wfName, wfNamespace)
				return err
			}
		}

		fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent - retryNumber: %d, workflow: %s, wfResourceVersion: %s, currentResourceVersion: %s, currentStatus: %s\n",
			time.Now().UTC().String(), retryNumber, wfName, wfResourceVersion, workflow.GetResourceVersion(), workflow.Status.String())
		retryNumber = retryNumber + 1

		workflow = workflow.DeepCopy()
		available := workflow.Status.GetCondition(api.RunningConditionType).IsTrue()
		fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent workflow: %s, available: %t\n", time.Now().UTC().String(), workflow.Name, available)

		if sfp, err = common.GetDataIndexPlatform(context.Background(), cli, workflow); err != nil {
			klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" It was not possible to get the DataIndex containing platform.", "workflow", "namespace", workflow.Name, workflow.Namespace)
			return err
		}
		if sfp == nil {
			klog.V(log.I).Infof("No DataIndex containing platform was found for workflow: %s, namespace: %s, to send the workflow definition status change event.", workflow.Name, workflow.Namespace)
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		evt := workflowdef.NewWorkflowDefinitionAvailabilityEvent(workflow, workflowdef.SonataFlowOperatorSource, properties.GetWorkflowEndpointUrl(workflow), available)
		if err = common.SendWorkflowDefinitionEvent(ctx, workflow, sfp, evt); err != nil {
			fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent, Error sending wf event: %s\n", time.Now().UTC().String(), workflow.Name)
			klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" Even delivery failed", "workflow", "namespace", workflow.Name, workflow.Namespace)
			// Controller handle to program a new notification based on the LastTimeStatusNotified.
			return err
		} else {
			now := metav1.Now()
			// Register the LastTimeStatusNotified, the controller knows how to react based on that value.
			workflow.Status.LastTimeStatusNotified = &now
			fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent, before cli.Status().Update(context.Background(), workflow) on workflow: %s, resourceVersion: %s\n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())
			if err = cli.Status().Update(context.Background(), workflow); err != nil {
				fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent, PerformStatusUpdate ERROR on workflow: %s, %s\n", time.Now().UTC().String(), workflow.Name, err.Error())
				klog.V(log.E).ErrorS(err, sendStatusUpdateGenericError+" Workflow status update failed.", "workflow", "namespace", workflow.Name, workflow.Namespace)
				return err
			} else {
				//TODO WM remove this else, only for printing the executed path
				fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent, after cli.Status().Update(context.Background(), workflow) on workflow: %s, resourceVersion: %s\n", time.Now().UTC().String(), workflow.Name, workflow.GetResourceVersion())
				fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent, PerformStatusUpdate OK on workflow: %s\n", time.Now().UTC().String(), workflow.Name)
				return nil
			}
		}
	})
	if retryErr != nil {
		fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent - workflow: %s, retryNumber: %d, retryErr: %s\n", time.Now().UTC().String(), wfName, retryNumber, retryErr.Error())
	} else {
		fmt.Printf("%s - deployment_handler.go.sendStatusUpdateEvent - workflow: %s, was done successful, no retryErrors, retryNumber: %d\n", time.Now().UTC().String(), wfName, retryNumber)
	}
}
