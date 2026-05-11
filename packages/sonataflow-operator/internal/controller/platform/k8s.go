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

package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/manager"

	v2 "k8s.io/api/autoscaling/v2"
	policyv1 "k8s.io/api/policy/v1"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/version"

	"k8s.io/klog/v2"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/metadata"

	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	eventingv1 "knative.dev/eventing/pkg/apis/eventing/v1"
	sourcesv1 "knative.dev/eventing/pkg/apis/sources/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/container-builder/client"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/knative"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform/services"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/constants"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/variables"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils"
	kubeutil "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils/kubernetes"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/workflowproj"
)

// NewServiceAction returns an action that deploys the services.
func NewServiceAction() Action {
	return &serviceAction{}
}

type serviceAction struct {
	baseAction
}

func (action *serviceAction) Name() string {
	return "service"
}

func (action *serviceAction) CanHandle(platform *operatorapi.SonataFlowPlatform) bool {
	return platform.Status.IsReady()
}

func (action *serviceAction) Handle(ctx context.Context, platform *operatorapi.SonataFlowPlatform) (*operatorapi.SonataFlowPlatform, *corev1.Event, error) {
	// Refresh applied configuration
	if err := CreateOrUpdateWithDefaults(ctx, platform, false); err != nil {
		return nil, nil, err
	}

	psDI := services.NewDataIndexHandler(platform)
	psJS := services.NewJobServiceHandler(platform)

	if IsJobsBasedDBMigration(platform, psDI, psJS) {
		p, err := HandleDBMigrationJob(ctx, action.client, platform, psDI, psJS)
		if p == nil && err == nil { // DB migration is in-progress
			return nil, nil, nil
		} else if p == nil && err != nil { // DB migration failed
			klog.V(log.E).ErrorS(err, "Error handling DB migration job", "namespace", platform.Namespace)
			return nil, nil, err
		}
	}

	if psDI.IsServiceSetInSpec() {
		if event, err := createOrUpdateServiceComponents(ctx, action.client, platform, psDI); err != nil {
			return nil, event, err
		}
	}

	createOrDeletePlatformRegistryWorker(action.client, platform, psDI.IsServiceEnabledInSpec())

	if psJS.IsServiceSetInSpec() {
		if event, err := createOrUpdateServiceComponents(ctx, action.client, platform, psJS); err != nil {
			return nil, event, err
		}
	}

	return platform, nil, nil
}

func createOrUpdateServiceComponents(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) (*corev1.Event, error) {
	var deployment *appsv1.Deployment
	var hpa *v2.HorizontalPodAutoscaler
	var err error
	if err = createOrUpdateConfigMap(ctx, client, platform, psh); err != nil {
		return nil, err
	}
	if deployment, hpa, err = createOrUpdateDeployment(ctx, client, platform, psh); err != nil {
		return nil, err
	}
	if err = createOrUpdatePDB(ctx, client, platform, psh, deployment, hpa); err != nil {
		return nil, err
	}
	if err = createOrUpdateService(ctx, client, platform, psh); err != nil {
		return nil, err
	}
	return createOrUpdateKnativeResources(ctx, client, platform, psh)
}

func createOrUpdateDeployment(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) (*appsv1.Deployment, *v2.HorizontalPodAutoscaler, error) {
	readyProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   constants.QuarkusHealthPathReady,
				Port:   variables.DefaultHTTPWorkflowPortIntStr,
				Scheme: corev1.URISchemeHTTP,
			},
		},
		InitialDelaySeconds: int32(45),
		TimeoutSeconds:      int32(10),
		PeriodSeconds:       int32(30),
		SuccessThreshold:    int32(1),
		FailureThreshold:    int32(4),
	}
	liveProbe := readyProbe.DeepCopy()
	liveProbe.ProbeHandler.HTTPGet.Path = constants.QuarkusHealthPathLive
	imageTag := psh.GetServiceImageName(constants.PersistenceTypeEphemeral)
	serviceContainer := &corev1.Container{
		Image:           imageTag,
		ImagePullPolicy: kubeutil.GetImagePullPolicy(imageTag),
		Env:             psh.GetEnvironmentVariables(),
		Resources:       psh.GetPodResourceRequirements(),
		ReadinessProbe:  readyProbe,
		LivenessProbe:   liveProbe,
		Ports: []corev1.ContainerPort{
			{
				Name:          utils.DefaultServicePortName,
				ContainerPort: int32(constants.DefaultHTTPWorkflowPortInt),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "application-config",
				MountPath: "/home/kogito/config",
			},
		},
	}
	serviceContainer = psh.ConfigurePersistence(serviceContainer)
	serviceContainer, err := psh.MergeContainerSpec(serviceContainer)
	if err != nil {
		return nil, nil, err
	}

	// immutable
	serviceContainer.Name = psh.GetContainerName()

	var hpa *v2.HorizontalPodAutoscaler = nil
	if psh.AcceptsHPA() {
		hpa, err = kubeutil.FindHPAForDeployment(ctx, utils.GetClient(), platform.Namespace, psh.GetServiceName())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find a potential HorizontalPodAutoscaler for deployment %s/%s: %v", platform.Namespace, psh.GetServiceName(), err)
		}
		klog.V(log.D).Infof("HorizontalPodAutoscaler exists for deployment %s/%s: %t.", platform.Namespace, psh.GetServiceName(), hpa != nil)
	}
	kSinkInjected, err := psh.CheckKSinkInjected()
	if err != nil {
		return nil, nil, err
	}
	lbl, selectorLbl := getLabels(platform, psh)
	serviceDeploymentSpec := appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: selectorLbl,
		},
		Strategy: psh.GetDeploymentStrategy(),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: lbl,
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{
						Name: "application-config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: psh.GetServiceCmName(),
								},
							},
						},
					},
				},
			},
		},
	}

	serviceDeploymentSpec.Template.Spec, err = psh.MergePodSpec(serviceDeploymentSpec.Template.Spec)
	if err != nil {
		return nil, nil, err
	}
	kubeutil.AddOrReplaceContainer(serviceContainer.Name, *serviceContainer, &serviceDeploymentSpec.Template.Spec)

	serviceDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: platform.Namespace,
			Name:      psh.GetServiceName(),
			Labels:    lbl,
		}}
	if err := controllerutil.SetControllerReference(platform, serviceDeployment, client.Scheme()); err != nil {
		return nil, nil, err
	}

	// Create or Update the deployment
	if op, err := controllerutil.CreateOrUpdate(ctx, client, serviceDeployment, func() error {
		knative.SaveKnativeData(&serviceDeploymentSpec.Template.Spec, &serviceDeployment.Spec.Template.Spec)
		err := mergo.Merge(&(serviceDeployment.Spec), serviceDeploymentSpec, mergo.WithOverride)
		// mergo.Merge algorithm is not setting the serviceDeployment.Spec.Replicas when the
		// *serviceDeploymentSpec.Replicas is 0. Making impossible to scale to zero. Ensure the value.
		if hpa == nil || !kubeutil.HPAIsWorking(hpa) || psh.GetReplicaCount() == 0 {
			// Only when no HorizontalPodAutoscaler was created for current deployment, we should manage the replicas.
			// Or, when the existing one did not wake up from a previous inactive period due to a replicas set to 0.
			// In this last case, we should still let the controller the chance to set the replicas to wake up the HorizontalPodAutoscaler.
			// Or, when the user voluntary wants to set the replicas to 0.
			replicas := psh.GetReplicaCount()
			if !kSinkInjected {
				replicas = 0 // Wait for K_SINK injection
			}
			serviceDeployment.Spec.Replicas = &replicas
		}
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	} else {
		klog.V(log.I).InfoS("Deployment successfully reconciled", "operation", op)
	}
	return serviceDeployment, hpa, nil
}

func createOrUpdatePDB(ctx context.Context, c client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler, deployment *appsv1.Deployment, hpa *v2.HorizontalPodAutoscaler) error {
	if psh.AcceptsPDB() {
		createOrUpdate := false
		pdbSpec := psh.GetPDBSpec()
		if !kubeutil.IsEmptyPodDisruptionBudgetSpec(pdbSpec) {
			if hpa != nil {
				// The HPA determines the replicas. Be sure that the service can't be later downscaled to a number of replicas that blocks a drain.
				// And also, that the user didn't voluntary scaled the service to 0.
				createOrUpdate = kubeutil.HPAMinReplicasIsGreaterThan(hpa, int32(1)) && !kubeutil.DeploymentIsScaledToZero(deployment)
			} else {
				// The just reconciled deployment replicas were already configured properly, we can rely on this number.
				// Be sure that the number of replicas don't block a drain.
				createOrUpdate = kubeutil.DeploymentReplicasIsGreaterThan(deployment, int32(1))
			}
		}

		if createOrUpdate {
			lbl, selectorLbl := getLabels(platform, psh)
			podDisruptionBudget := &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      psh.GetServiceName(),
					Namespace: platform.Namespace,
					Labels:    lbl,
				},
			}
			if err := controllerutil.SetControllerReference(platform, podDisruptionBudget, c.Scheme()); err != nil {
				return err
			}
			if op, err := controllerutil.CreateOrUpdate(ctx, c, podDisruptionBudget, func() error {
				kubeutil.ApplyPodDisruptionBudgetSpec(podDisruptionBudget, pdbSpec)
				podDisruptionBudget.Spec.Selector = &metav1.LabelSelector{
					MatchLabels: selectorLbl,
				}
				return nil
			}); err != nil {
				return err
			} else {
				klog.V(log.I).Infof("PodDisruptionBudget %s/%s successfully reconciled, op: %s.", podDisruptionBudget.Namespace, podDisruptionBudget.Name, op)
			}
		} else {
			// Delete a potentially existing PDB.
			if err := kubeutil.SafeDeletePodDisruptionBudget(ctx, c, platform.Namespace, psh.GetServiceName()); err != nil {
				return err
			}
		}
	}
	return nil
}

func createOrUpdateService(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) error {
	lbl, selectorLbl := getLabels(platform, psh)
	dataSvcSpec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       utils.DefaultServicePortName,
				Protocol:   corev1.ProtocolTCP,
				Port:       80,
				TargetPort: variables.DefaultHTTPWorkflowPortIntStr,
			},
		},
		Selector: selectorLbl,
	}
	dataSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: platform.Namespace,
			Name:      psh.GetServiceName(),
			Labels:    lbl,
		}}
	if err := controllerutil.SetControllerReference(platform, dataSvc, client.Scheme()); err != nil {
		return err
	}

	// Create or Update the service
	if op, err := controllerutil.CreateOrUpdate(ctx, client, dataSvc, func() error {
		dataSvc.Spec = dataSvcSpec

		return nil
	}); err != nil {
		return err
	} else {
		klog.V(log.I).InfoS("Service successfully reconciled", "operation", op)
	}

	return nil
}

func createOrUpdatePlatformRegistry(ctx context.Context, c client.Client, platform *operatorapi.SonataFlowPlatform) {
	registry := manager.GetSFPControllerWorkerRegistry()
	workerName := fmt.Sprintf("%s-%s", platform.Namespace, platform.Name)
	if !registry.Exists(workerName) {
		worker := manager.NewPeriodicWorker(func(ctx context.Context) {
			fmt.Printf("XXXX executing worker! %s\n", workerName)
			return
		}, 2, time.Duration(5*time.Second))
		registry.Register(workerName, worker)
		worker.Start(registry.GetRootContext())
	}
}

func workerName(namespace string) string {
	return fmt.Sprintf("%s-worker", namespace)
}

func workflowVersionsConfigMapName(namespace string) string {
	return "sonataflow-platform-registry"
}

func createOrDeletePlatformRegistryWorker(c client.Client, platform *operatorapi.SonataFlowPlatform, create bool) {
	klog.V(log.D).Infof("createOrDeletePlatformRegistryWorker for : %s/%s, create: %t", platform.Namespace, platform.Name, create)
	registry := manager.GetSFPControllerWorkerRegistry()
	platformWorkerName := workerName(platform.Namespace)
	if !create {
		klog.V(log.D).Infof("Try to Deregister worker: %s.", platformWorkerName)
		worker := registry.GetIfExists(platformWorkerName)
		if worker != nil {
			klog.V(log.D).Infof("Deregister existing worker: %s.", platformWorkerName)
			registry.Deregister(platformWorkerName)
			worker.Stop()
		} else {
			klog.V(log.D).Infof("Worker: %s is not registered, it could have been Deregistered in a former recon cycle.", platformWorkerName)
		}
	} else {
		klog.V(log.D).Infof("Try to Register worker: %s.", platformWorkerName)
		worker := registry.GetIfExists(platformWorkerName)
		if worker != nil {
			klog.V(log.D).Infof("Worker: %s was already registered.", platformWorkerName)
		} else {
			klog.V(log.D).Infof("Worker: %s is not registered, it must be registered now.", platformWorkerName)
			worker = manager.NewPeriodicWorker(refreshRegistry(c, platform.Namespace, platform.Name), 2, time.Duration(5*time.Second))
			registry.Register(platformWorkerName, worker)
			worker.Start(registry.GetRootContext())
		}
	}
}

func GetDataIndexGraphqlURL(cli client.Client, namespace string) (string, error) {
	var err error
	var sfp *operatorapi.SonataFlowPlatform
	var uri string

	if sfp, err = GetActivePlatform(context.Background(), cli, namespace, false); err != nil {
		return "", fmt.Errorf("failed to get active platform for namespace: %s, %v", namespace, err)
	}
	if sfp == nil {
		klog.V(log.D).Infof("No active platform was found to query the workflow definitions for namespace: %s.", namespace)
		return "", err
	}
	diHandler := services.NewDataIndexHandler(sfp)
	if !diHandler.IsServiceEnabledInSpec() {
		klog.V(log.D).Infof("DataIndex is not enabled for namespace: %s.", namespace)
		return "", nil
	}
	uri = diHandler.GetServiceBaseUrl() + "/graphql"
	return uri, nil
}

func refreshRegistry(cli client.Client, namespace, name string) manager.ContextAwareRunnable {
	return func(ctx context.Context) {
		wName := workerName(namespace)
		klog.V(log.D).Infof("worker: %s is executing the periodic registry refresh for namespace: %s.", wName, namespace)
		graphqlURL, err := GetDataIndexGraphqlURL(cli, namespace)
		if err != nil {
			klog.V(log.W).Infof("worker: %s failed to get Data Index graphqlURL for namespace: %s. A new retry will be executed in the next period, %v", wName, namespace, err)
			return
		}
		klog.V(log.D).Infof("worker: %s successfully obtained the Data Index graphqlURL for namespace: %s -> %s.", wName, namespace, graphqlURL)

		query := GraphQLQuery{Query: "{ ProcessDefinitions { id, version } }"}
		response, err := ExecuteDataIndexQuery(ctx, graphqlURL, query)
		if err != nil {
			klog.V(log.W).Infof("worker: %s failed to execute workflow definitions Data Index query for namespace: %s, a new attempt will be executed in the next period: %v.", wName, namespace, err)
			return
		}
		data := response.Data
		if data == nil {
			klog.V(log.W).Infof("worker: %s workflow definitions Data Index query returned no data field.", wName)
			return
		}

		rawDefs, ok := data["ProcessDefinitions"]
		if !ok || rawDefs == nil {
			klog.V(log.W).Infof("worker: %s, no workflow definitions are registered in the Data Index namespace: %s.", wName, namespace)
			return
		}

		defs, ok := rawDefs.([]interface{})
		if !ok {
			klog.V(log.W).Infof("worker: %s, workflow definitions query returned no 'ProcessDefinitions' field, or its not an array, for namespace: %s.", wName, namespace)
			return
		}

		workflows := make(Workflows)
		for _, item := range defs {
			obj, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			wfId, _ := obj["id"].(string)
			wfVersion, _ := obj["version"].(string)

			klog.V(log.D).Infof("worker: %s, workflow definition was found: (%s, %s) in the Data Index query result.", wName, wfId, wfVersion)

			currentWfVersions := workflows[wfId]
			currentWfVersions = append(currentWfVersions, Version{
				Version: wfVersion,
				Enabled: nil,
			})
			workflows[wfId] = currentWfVersions
		}
		klog.V(log.D).Infof("worker: %s, workflow definitions query returned %d workflow ids", wName, len(workflows))
		err = createOrUpdateWorkflowVersionsConfigMap(ctx, cli, workflowVersionsConfigMapName(namespace), namespace, workflows)
		if err != nil {
			klog.V(log.W).Infof("worker: %s failed to execute createOrUpdateWorkflowVersionsConfigMap for namespace: %s, a new attempt will be executed in the next period: %v.", wName, namespace, err)
		}
	}
}

func createOrUpdateWorkflowVersionsConfigMap(ctx context.Context, cli client.Client, name string, namespace string, workflows Workflows) error {
	var skip SkipConfig

	klog.V(log.D).Infof("We must marshal %d workflows", len(workflows))

	wfYAML, err := MarshalWorkflows(workflows)
	if err != nil {
		return fmt.Errorf("failed to marshal workflow versions information: %v", err)
	}

	skipYAML, err := MarshalSkipConfig(skip)
	if err != nil {
		return fmt.Errorf("failed to marshal the workflow that skips the version validation: %v", err)
	}

	wfYAMLMarshalled := string(wfYAML)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			//Labels:    GetMergedLabels(workflow),
		},
		Data: map[string]string{
			WorkflowsField: wfYAMLMarshalled,
			// TODO remove, we dont set the user provided config.
			SkipField: string(skipYAML),
		},
	}

	if op, err := controllerutil.CreateOrUpdate(ctx, cli, configMap, func() error {
		// always write the versions, and let the potential user entered skip.yaml untouched
		if configMap.Data == nil {
			configMap.Data = map[string]string{}
		}
		configMap.Data[WorkflowsField] = wfYAMLMarshalled
		return nil
	}); err != nil {
		return err
	} else {
		klog.V(log.I).Infof("SonataPlatformRegistry ConfigMap %s/%s successfully reconciled, op: %s.", configMap.Namespace, configMap.Name, op)
	}
	return nil
}

func createOrUpdateRegistry(ctx context.Context, c client.Client, platform *operatorapi.SonataFlowPlatform) error {
	name := platform.Name
	lbl := map[string]string{
		workflowproj.LabelApp:             platform.Name,
		workflowproj.LabelAppNamespace:    platform.Namespace,
		metadata.KubernetesLabelInstance:  platform.Name,
		metadata.KubernetesLabelName:      name,
		metadata.KubernetesLabelComponent: "registry",
		metadata.KubernetesLabelPartOf:    platform.Name,
		metadata.KubernetesLabelManagedBy: "sonataflow-operator",
		metadata.KubernetesLabelVersion:   version.GetImageTagVersion(),
	}

	registry := &operatorapi.SonataFlowRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: platform.Namespace,
			Labels:    lbl,
		},
	}

	if err := controllerutil.SetControllerReference(platform, registry, c.Scheme()); err != nil {
		return err
	}

	if op, err := controllerutil.CreateOrUpdate(ctx, c, registry, func() error {
		return nil
	}); err != nil {
		return err
	} else {
		klog.V(log.I).Infof("SonataFlowRegistry %s/%s successfully reconciled, op: %s.", registry.Namespace, registry.Name, op)
	}
	return nil
}

// getServicesLabelsMap A common utility function for use by SonataFlow Services (e.g. DI/JS and DB Migrator) to obtain standard common labels by passing parameters
func getServicesLabelsMap(app string, appNamespace string, service string, k8sName string, k8sComponent string, k8sPartOf string, k8sManagedBy string) (map[string]string, map[string]string) {
	lbl := map[string]string{
		workflowproj.LabelApp:             app,
		workflowproj.LabelAppNamespace:    appNamespace,
		workflowproj.LabelService:         service,
		metadata.KubernetesLabelInstance:  app,
		metadata.KubernetesLabelName:      k8sName,
		metadata.KubernetesLabelComponent: k8sComponent,
		metadata.KubernetesLabelPartOf:    k8sPartOf,
		metadata.KubernetesLabelManagedBy: k8sManagedBy,
		metadata.KubernetesLabelVersion:   version.GetImageTagVersion(),
	}

	selectorLbl := map[string]string{
		workflowproj.LabelService: service,
	}

	return lbl, selectorLbl
}

// getLabels Specifically used by services implementing services.PlatformServiceHandler interface such as DI/JS
func getLabels(platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) (map[string]string, map[string]string) {
	return getServicesLabelsMap(platform.Name, platform.Namespace, psh.GetServiceName(), psh.GetContainerName(), psh.GetServiceName(), platform.Name, "sonataflow-operator")
}

func createOrUpdateConfigMap(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) error {
	handler, err := services.NewServiceAppPropertyHandler(psh)
	if err != nil {
		return err
	}
	lbl, _ := getLabels(platform, psh)
	dataStr := handler.Build()
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      psh.GetServiceCmName(),
			Namespace: platform.Namespace,
			Labels:    lbl,
		},
		Data: map[string]string{
			workflowproj.ApplicationPropertiesFileName: dataStr,
		},
	}
	if err := controllerutil.SetControllerReference(platform, configMap, client.Scheme()); err != nil {
		return err
	}

	// Create or Update the service
	if op, err := controllerutil.CreateOrUpdate(ctx, client, configMap, func() error {
		configMap.Data[workflowproj.ApplicationPropertiesFileName] = handler.WithUserProperties(dataStr).Build()

		return nil
	}); err != nil {
		return err
	} else {
		klog.V(log.I).InfoS("ConfigMap successfully reconciled", "operation", op)
	}
	return nil
}

func setSonataFlowPlatformFinalizer(ctx context.Context, c client.Client, platform *operatorapi.SonataFlowPlatform) error {
	if !controllerutil.ContainsFinalizer(platform, constants.TriggerFinalizer) {
		controllerutil.AddFinalizer(platform, constants.TriggerFinalizer)
		return c.Update(ctx, platform)
	}
	return nil
}

func createOrUpdateKnativeResources(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psh services.PlatformServiceHandler) (*corev1.Event, error) {
	lbl, _ := getLabels(platform, psh)
	objs, event, err := psh.GenerateKnativeResources(platform, lbl)
	if err != nil {
		return event, err
	}
	// Create or update triggers
	for _, obj := range objs {
		if triggerDef, ok := obj.(*eventingv1.Trigger); ok {
			if platform.Namespace == obj.GetNamespace() {
				if err := controllerutil.SetControllerReference(platform, obj, client.Scheme()); err != nil {
					return nil, err
				}
			} else {
				// This is for Knative trigger in a different namespace
				// Set the finalizer for trigger cleanup when the platform is deleted
				if err := setSonataFlowPlatformFinalizer(ctx, client, platform); err != nil {
					return nil, err
				}
			}
			trigger := &eventingv1.Trigger{
				ObjectMeta: triggerDef.ObjectMeta,
			}
			_, err := controllerutil.CreateOrUpdate(ctx, client, trigger, func() error {
				trigger.Spec = triggerDef.Spec
				return nil
			})
			if err != nil {
				return nil, err
			}
			addToSonataFlowPlatformTriggerList(platform, trigger)
		}
	}

	if err := SafeUpdatePlatformStatus(ctx, platform); err != nil {
		return nil, err
	}

	// Create or update sinkbindings
	for _, obj := range objs {
		if sbDef, ok := obj.(*sourcesv1.SinkBinding); ok {
			if err := controllerutil.SetControllerReference(platform, obj, client.Scheme()); err != nil {
				return nil, err
			}
			sinkBinding := &sourcesv1.SinkBinding{
				ObjectMeta: sbDef.ObjectMeta,
			}
			_, err = controllerutil.CreateOrUpdate(ctx, client, sinkBinding, func() error {
				sinkBinding.Spec = sbDef.Spec
				return nil
			})
			if err != nil {
				return nil, err
			}
			kSinkInjected, err := psh.CheckKSinkInjected()
			if err != nil {
				return nil, err
			}
			if !kSinkInjected {
				msg := fmt.Sprintf("waiting for K_SINK injection for service %s to complete", psh.GetServiceName())
				event := &corev1.Event{
					Type:    corev1.EventTypeWarning,
					Reason:  services.WaitingKnativeEventing,
					Message: msg,
				}
				return event, fmt.Errorf("%s", msg)
			}
		}
	}
	return nil, nil
}

func addToSonataFlowPlatformTriggerList(platform *operatorapi.SonataFlowPlatform, trigger *eventingv1.Trigger) {
	for _, t := range platform.Status.Triggers {
		if t.Name == trigger.Name && t.Namespace == trigger.Namespace {
			return // trigger already exists
		}
	}
	platform.Status.Triggers = append(platform.Status.Triggers, operatorapi.SonataFlowPlatformTriggerRef{Name: trigger.Name, Namespace: trigger.Namespace})
}
