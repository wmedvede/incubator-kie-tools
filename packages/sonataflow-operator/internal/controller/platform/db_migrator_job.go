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
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/utils/kubernetes"

	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/version"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	operatorapi "github.com/apache/incubator-kie-tools/packages/sonataflow-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/container-builder/client"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/cfg"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/platform/services"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/constants"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/internal/controller/profiles/common/persistence"
	"github.com/apache/incubator-kie-tools/packages/sonataflow-operator/log"
)

type QuarkusDataSource struct {
	JdbcUrl           string
	SecretRefName     string
	SecretUserKey     string
	SecretPasswordKey string
	Schema            string
}

type DBMigratorJobData struct {
	MigrateDBDataIndex    bool
	DataIndexDataSource   *QuarkusDataSource
	MigrateDBJobsService  bool
	JobsServiceDataSource *QuarkusDataSource
}

type DBMigratorJob struct {
	Name string
	Data DBMigratorJobData
}

type DBMigratorJobStatus struct {
	Name           string
	BatchJobStatus *batchv1.JobStatus
}

const (
	dbMigrationJobName       = "sonataflow-db-migrator-job"
	dbMigrationContainerName = "db-migration-container"
	dbMigrationJobFailed     = 1
	dbMigrationJobSucceeded  = 1

	migrateDBDataIndex                 = "MIGRATE_DB_DATAINDEX"
	quarkusDataSourceDataIndexJdbcURL  = "QUARKUS_DATASOURCE_DATAINDEX_JDBC_URL"
	quarkusDataSourceDataIndexUserName = "QUARKUS_DATASOURCE_DATAINDEX_USERNAME"
	quarkusDataSourceDataIndexPassword = "QUARKUS_DATASOURCE_DATAINDEX_PASSWORD"
	quarkusFlywayDataIndexSchemas      = "QUARKUS_FLYWAY_DATAINDEX_SCHEMAS"

	migrateDBJobsService                 = "MIGRATE_DB_JOBSSERVICE"
	quarkusDataSourceJobsServiceJdbcURL  = "QUARKUS_DATASOURCE_JOBSSERVICE_JDBC_URL"
	quarkusDataSourceJobsServiceUserName = "QUARKUS_DATASOURCE_JOBSSERVICE_USERNAME"
	quarkusDataSourceJobsServicePassword = "QUARKUS_DATASOURCE_JOBSSERVICE_PASSWORD"
	quarkusFlywayJobsServiceSchemas      = "QUARKUS_FLYWAY_JOBSSERVICE_SCHEMAS"
	defaultPostgreSqlUserKey             = "POSTGRESQL_USER"
	defaultPostgreSqlPassworkdKey        = "POSTGRESQL_PASSWORD"
)

type DBMigrationJobCfg struct {
	JobName       string
	ContainerName string
	ToolImageName string
}

// getDbMigratorJobName returns the required name for the DB migrator Job, considering the following:
// Every Job represents a batch unit of work. After executed, or during execution, it makes no sense the change the
// definition (Spec), since it's basically already executed work. (In fact, Kubernetes won't generate a new Pod).
// For every new product version, similar to Quarkus embedded flyway execution, we want to give the chance for the
// migration Job to execute, since .sql changes might come. But, old already executed Jobs shouldn't be affected.
// The following strategy will facilitate migrations, and potential Job definition changes in the same
// version. (This last normally doesn't happen)
// If we have a DBMigrationJob, and we are for example in version 1.38.0, the following Job is created
// sonataflow-db-migrator-job-1.38.0-7c9f4b21
// Where:
// The prefix "sontaflow-db-migrator" is fixed.
// The 1.38.0 is the current application version name.
// The suffix 7c9f4b21 is the hash of the relevant data that composes the work to do. (i.e. the DBMigratorJobData)
// In the future, when a new version 1.39.0 is executing, for the same SFP, a new
// sonataflow-db-migrator-job-1.39.0-9dc9g4b25 will be generated, giving the chance to execute the flyway migration.
func getDbMigratorJobName(data *DBMigratorJobData) (string, error) {
	hash, err := hashDBMigratorJobData(data)
	if err != nil {
		return "", fmt.Errorf("failed to calculate sonataflow-db-migrator job name: %v", err)
	}
	return fmt.Sprintf("%s-%s-%s", dbMigrationJobName, version.GetImageTagVersion(), hash), nil
}

// HashDBMigratorJob returns an 8-char hex hash based on the DBMigratorJob definition.
func hashDBMigratorJobData(job *DBMigratorJobData) (string, error) {
	// marshal struct to JSON
	b, err := json.Marshal(job)
	if err != nil {
		return "", err
	}

	// FNV-32a hash
	h := fnv.New32a()
	_, err = h.Write(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum32()), nil
}

func getJdbcUrl(env []corev1.EnvVar) string {
	if env != nil {
		for i := 0; i < len(env); i++ {
			if env[i].Name == "QUARKUS_DATASOURCE_JDBC_URL" {
				return env[i].Value
			}
		}
	}
	return ""
}

// getQuarkusDSFromServicePersistence Returns QuarkusDataSource from service level persistence config
func getQuarkusDSFromServicePersistence(platform *operatorapi.SonataFlowPlatform, persistenceOptionsSpec *operatorapi.PersistenceOptionsSpec, defaultSchemaName string) *QuarkusDataSource {
	klog.InfoS("Using service level persistence for PostgreSQL", "defaultSchemaName", defaultSchemaName)
	quarkusDataSource := &QuarkusDataSource{}
	env := persistence.ConfigurePostgreSQLEnv(persistenceOptionsSpec.PostgreSQL, defaultSchemaName, platform.Namespace, false)
	quarkusDataSource.JdbcUrl = getJdbcUrl(env)
	quarkusDataSource.SecretRefName = persistenceOptionsSpec.PostgreSQL.SecretRef.Name
	quarkusDataSource.SecretUserKey = defaultPostgreSqlUserKey
	if len(persistenceOptionsSpec.PostgreSQL.SecretRef.UserKey) > 0 {
		quarkusDataSource.SecretUserKey = persistenceOptionsSpec.PostgreSQL.SecretRef.UserKey
	}
	quarkusDataSource.SecretPasswordKey = defaultPostgreSqlPassworkdKey
	if len(persistenceOptionsSpec.PostgreSQL.SecretRef.PasswordKey) > 0 {
		quarkusDataSource.SecretPasswordKey = persistenceOptionsSpec.PostgreSQL.SecretRef.PasswordKey
	}
	quarkusDataSource.Schema = persistence.GetDBSchemaName(persistenceOptionsSpec.PostgreSQL, defaultSchemaName)
	return quarkusDataSource
}

// getQuarkusDSFromPlatformPersistence Returns QuarkusDataSource from platform level persistence config
func getQuarkusDSFromPlatformPersistence(platform *operatorapi.SonataFlowPlatform, defaultSchemaName string) *QuarkusDataSource {
	klog.InfoS("Using platform level persistence for PostgreSQL", "defaultSchemaName", defaultSchemaName)
	quarkusDataSource := &QuarkusDataSource{}
	postgresql := persistence.MapToPersistencePostgreSQL(platform, defaultSchemaName)

	env := persistence.ConfigurePostgreSQLEnv(postgresql, defaultSchemaName, platform.Namespace, false)
	quarkusDataSource.JdbcUrl = getJdbcUrl(env)
	quarkusDataSource.SecretRefName = platform.Spec.Persistence.PostgreSQL.SecretRef.Name
	quarkusDataSource.SecretUserKey = defaultPostgreSqlUserKey
	if len(platform.Spec.Persistence.PostgreSQL.SecretRef.UserKey) > 0 {
		quarkusDataSource.SecretUserKey = platform.Spec.Persistence.PostgreSQL.SecretRef.UserKey
	}
	quarkusDataSource.SecretPasswordKey = defaultPostgreSqlPassworkdKey
	if len(platform.Spec.Persistence.PostgreSQL.SecretRef.PasswordKey) > 0 {
		quarkusDataSource.SecretPasswordKey = platform.Spec.Persistence.PostgreSQL.SecretRef.PasswordKey
	}
	quarkusDataSource.Schema = persistence.GetDBSchemaName(postgresql, defaultSchemaName)
	return quarkusDataSource
}

// getQuarkusDataSourceFromPersistence PostgreSQL persistence can be defined at platform level (where both DI and JS will use the same DB defined at platform level) or db can defined at Service level. Service level config will take precedence over platform level config.
func getQuarkusDataSourceFromPersistence(platform *operatorapi.SonataFlowPlatform, persistenceOptionsSpec *operatorapi.PersistenceOptionsSpec, defaultSchemaName string) *QuarkusDataSource {

	if persistenceOptionsSpec != nil && persistenceOptionsSpec.PostgreSQL != nil {
		return getQuarkusDSFromServicePersistence(platform, persistenceOptionsSpec, defaultSchemaName)
	} else if platform != nil && platform.Spec.Persistence != nil && platform.Spec.Persistence.PostgreSQL != nil {
		return getQuarkusDSFromPlatformPersistence(platform, defaultSchemaName)
	}

	return nil
}

// NewDBMigratorJobData given a SFP, and the respective Job Service and Data Index service handlers, returns the relevant
// information for creating the corresponding DB migration Job. In cases where the "job" based DB migration strategy is not
// configured returns nil.
func NewDBMigratorJobData(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, pshDI services.PlatformServiceHandler, pshJS services.PlatformServiceHandler) *DBMigratorJobData {

	diJobsBasedDBMigration := false
	jsJobsBasedDBMigration := false

	if pshDI.IsPersistenceEnabledtInSpec() {
		diJobsBasedDBMigration = services.IsJobsBasedDBMigration(platform.Spec.Services.DataIndex.Persistence)
	}
	if pshJS.IsPersistenceEnabledtInSpec() {
		jsJobsBasedDBMigration = services.IsJobsBasedDBMigration(platform.Spec.Services.JobService.Persistence)
	}

	if (pshDI.IsServiceSetInSpec() && diJobsBasedDBMigration) || (pshJS.IsServiceSetInSpec() && jsJobsBasedDBMigration) {
		quarkusDataSourceDataIndex := &QuarkusDataSource{}
		quarkusDataSourceJobService := &QuarkusDataSource{}

		if diJobsBasedDBMigration {
			quarkusDataSourceDataIndex = getQuarkusDataSourceFromPersistence(platform, platform.Spec.Services.DataIndex.Persistence, pshDI.GetServiceName())
		}

		if jsJobsBasedDBMigration {
			quarkusDataSourceJobService = getQuarkusDataSourceFromPersistence(platform, platform.Spec.Services.JobService.Persistence, pshJS.GetServiceName())
		}

		return &DBMigratorJobData{
			MigrateDBDataIndex:    diJobsBasedDBMigration,
			DataIndexDataSource:   quarkusDataSourceDataIndex,
			MigrateDBJobsService:  jsJobsBasedDBMigration,
			JobsServiceDataSource: quarkusDataSourceJobService,
		}
	}
	return nil
}

// IsJobsBasedDBMigration returns whether job based db migration approach is needed?
func IsJobsBasedDBMigration(platform *operatorapi.SonataFlowPlatform, pshDI services.PlatformServiceHandler, pshJS services.PlatformServiceHandler) bool {
	diJobsBasedDBMigration := false
	jsJobsBasedDBMigration := false

	if pshDI.IsPersistenceEnabledtInSpec() {
		diJobsBasedDBMigration = services.IsJobsBasedDBMigration(platform.Spec.Services.DataIndex.Persistence)
	}
	if pshJS.IsPersistenceEnabledtInSpec() {
		jsJobsBasedDBMigration = services.IsJobsBasedDBMigration(platform.Spec.Services.JobService.Persistence)
	}

	return (pshDI.IsServiceSetInSpec() && diJobsBasedDBMigration) || (pshJS.IsServiceSetInSpec() && jsJobsBasedDBMigration)
}

func createOrUpdateDBMigrationJob(ctx context.Context, cli client.Client, platform *operatorapi.SonataFlowPlatform, pshDI services.PlatformServiceHandler, pshJS services.PlatformServiceHandler) (*DBMigratorJob, error) {
	dbMigratorJobData := NewDBMigratorJobData(ctx, cli, platform, pshDI, pshJS)
	// Invoke DB Migration only if both or either DI/JS services are requested, in addition to DBMigrationStrategyJob
	if dbMigratorJobData != nil {
		var dbMigratorJob *DBMigratorJob
		// Get the expected Job name for the current product version and the relevant data.
		dbMigratorJobName, err := getDbMigratorJobName(dbMigratorJobData)
		if err != nil {
			return nil, err
		}
		dbMigratorJob = &DBMigratorJob{
			Name: dbMigratorJobName,
			Data: *dbMigratorJobData,
		}
		currentK8sMigratorJob, err := kubernetes.FindJob(ctx, cli, platform.Namespace, dbMigratorJob.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to verify if the job: %s/%s already exists, %v", platform.Namespace, dbMigratorJob.Name, err)
		}
		fmt.Printf("SHALL WE NEED TO CREATE THE JOB %s = %t\n", dbMigratorJob.Name, currentK8sMigratorJob == nil)
		//TODO, si existiera un job previo ejecutando, todavia no podeos crear el nuevo
		if currentK8sMigratorJob == nil {

			// Delete DI and JS deployments for safety, we must avoid serving requests during the DB schema migration. (Unexpected results, data, etc., might happen)
			// Both will be recreated in upcoming recon cycle after the Job finishes. (we can keep the respective Services if already created to keep better response time)
			if err = kubernetes.SafeDeleteDeployment(ctx, cli, platform.Namespace, pshDI.GetServiceName()); err != nil {
				return nil, fmt.Errorf("failed to delete DI deployment: %s/%s, %v", platform.Namespace, pshDI.GetServiceName(), err)
			}
			if err = kubernetes.SafeDeleteDeployment(ctx, cli, platform.Namespace, pshJS.GetServiceName()); err != nil {
				return nil, fmt.Errorf("failed to delete JS deployment: %s/%s, %v", platform.Namespace, pshDI.GetServiceName(), err)
			}
			job := createJobDBMigration(platform, dbMigratorJob)
			klog.V(log.I).InfoS("Creating DB Migration Job: ", "namespace", platform.Namespace, "job", job.Name)
			if err := controllerutil.SetControllerReference(platform, job, cli.Scheme()); err != nil {
				return nil, fmt.Errorf("failed to set controller reference on job: %s/%s, %v", platform.Namespace, dbMigratorJob.Name, err)
			}
			if op, err := controllerutil.CreateOrUpdate(ctx, cli, job, func() error {
				return nil
			}); err != nil {
				return dbMigratorJob, err
			} else {
				klog.V(log.I).InfoS("DB Migration Job successfully created on cluster", "operation", op, "namespace", platform.Namespace, "job", job.Name)
			}
		} else {
			klog.V(log.D).InfoS("DB Migration Job already exits: ", "namespace", platform.Namespace, "job", dbMigratorJobName)
		}
		return dbMigratorJob, nil
	} else {
		return nil, nil
	}
}

// HandleDBMigrationJob Creates db migration job and executes it on the cluster
func HandleDBMigrationJob(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, psDI services.PlatformServiceHandler, psJS services.PlatformServiceHandler) (*operatorapi.SonataFlowPlatform, error) {
	runningJob, err := findRunningMigratorJobInNamespace(ctx, client, platform.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to find a potential running migration job in namespace: %s, %v", platform, err)
	}
	if runningJob != nil {
		// avoid having migration jobs running in parallel
		dbMigratorJobStatus, err := ReconcileDBMigrationJob(ctx, client, platform, runningJob.Name)
		if err != nil {
			return nil, err
		}
		if hasFailed(dbMigratorJobStatus) {
			return nil, errors.New("DB migration job " + dbMigratorJobStatus.Name + " failed in namespace: " + platform.Namespace)
		} else if hasSucceeded(dbMigratorJobStatus) {
			return platform, nil
		} else {
			// DB migration is still running, a new recon will come.
			return nil, nil
		}
	}

	dbMigratorJob, err := createOrUpdateDBMigrationJob(ctx, client, platform, psDI, psJS)
	if err != nil {
		return nil, err
	}
	if dbMigratorJob != nil {
		klog.V(log.E).InfoS("Created DB migration job")
		dbMigratorJobStatus, err := ReconcileDBMigrationJob(ctx, client, platform)
		if err != nil {
			return nil, err
		}
		if hasFailed(dbMigratorJobStatus) {
			return nil, errors.New("DB migration job " + dbMigratorJobStatus.Name + " failed in namespace: " + platform.Namespace)
		} else if hasSucceeded(dbMigratorJobStatus) {
			return platform, nil
		} else {
			// DB migration is still running
			return nil, nil
		}
	}

	return platform, nil
}

func newQuarkusDataSource(jdbcURL string, secretRefName string, secretUserKey string, secretPasswordKey string, schema string) *QuarkusDataSource {
	return &QuarkusDataSource{
		JdbcUrl:           jdbcURL,
		SecretRefName:     secretRefName,
		SecretUserKey:     secretUserKey,
		SecretPasswordKey: secretPasswordKey,
		Schema:            schema,
	}
}

func createJobDBMigration(platform *operatorapi.SonataFlowPlatform, dbmj *DBMigratorJob) *batchv1.Job {
	// In DB Migrator Tool, smallrye will throw error for empty string "" while initializing properties.
	// So use an empty space as a default value. Please see more at: https://github.com/eclipse/microprofile-config/issues/671
	nonEmptyValue := " "
	diQuarkusDataSource := newQuarkusDataSource(nonEmptyValue, nonEmptyValue, nonEmptyValue, nonEmptyValue, nonEmptyValue)
	jsQuarkusDataSource := newQuarkusDataSource(nonEmptyValue, nonEmptyValue, nonEmptyValue, nonEmptyValue, nonEmptyValue)

	if dbmj.Data.MigrateDBDataIndex && dbmj.Data.DataIndexDataSource != nil {
		diQuarkusDataSource.JdbcUrl = dbmj.Data.DataIndexDataSource.JdbcUrl
		diQuarkusDataSource.SecretRefName = dbmj.Data.DataIndexDataSource.SecretRefName
		diQuarkusDataSource.SecretUserKey = dbmj.Data.DataIndexDataSource.SecretUserKey
		diQuarkusDataSource.SecretPasswordKey = dbmj.Data.DataIndexDataSource.SecretPasswordKey
		diQuarkusDataSource.Schema = dbmj.Data.DataIndexDataSource.Schema
	}

	if dbmj.Data.MigrateDBJobsService && dbmj.Data.JobsServiceDataSource != nil {
		jsQuarkusDataSource.JdbcUrl = dbmj.Data.JobsServiceDataSource.JdbcUrl
		jsQuarkusDataSource.SecretRefName = dbmj.Data.JobsServiceDataSource.SecretRefName
		jsQuarkusDataSource.SecretUserKey = dbmj.Data.JobsServiceDataSource.SecretUserKey
		jsQuarkusDataSource.SecretPasswordKey = dbmj.Data.JobsServiceDataSource.SecretPasswordKey
		jsQuarkusDataSource.Schema = dbmj.Data.JobsServiceDataSource.Schema
	}

	diDBSecretRef := corev1.LocalObjectReference{
		Name: diQuarkusDataSource.SecretRefName,
	}

	jsDBSecretRef := corev1.LocalObjectReference{
		Name: jsQuarkusDataSource.SecretRefName,
	}

	dbMigrationJobCfg := newDBMigrationJobCfg(dbmj.Name)

	fmt.Printf("XXXXX DB Mibrator JOB must be created with image: %s\n", dbMigrationJobCfg.ToolImageName)

	lbl, _ := getServicesLabelsMap(platform.Name, platform.Namespace, fmt.Sprintf("%s-%s", "sonataflow-db-job", dbMigrationJobCfg.JobName), dbMigrationJobCfg.JobName, fmt.Sprintf("%s-%s", platform.Name, dbMigrationJobCfg.JobName), platform.Name, "sonataflow-operator")

	envVars := make([]corev1.EnvVar, 0)
	envVars = append(envVars, corev1.EnvVar{
		Name:  migrateDBDataIndex,
		Value: strconv.FormatBool(dbmj.Data.MigrateDBDataIndex),
	})
	if dbmj.Data.MigrateDBDataIndex {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  quarkusDataSourceDataIndexJdbcURL,
				Value: diQuarkusDataSource.JdbcUrl,
			},
			corev1.EnvVar{
				Name: quarkusDataSourceDataIndexUserName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  diQuarkusDataSource.SecretUserKey,
						LocalObjectReference: diDBSecretRef,
					},
				},
			},
			corev1.EnvVar{
				Name: quarkusDataSourceDataIndexPassword,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  diQuarkusDataSource.SecretPasswordKey,
						LocalObjectReference: diDBSecretRef,
					},
				},
			},
			corev1.EnvVar{
				Name:  quarkusFlywayDataIndexSchemas,
				Value: diQuarkusDataSource.Schema,
			})
	}

	envVars = append(envVars, corev1.EnvVar{
		Name:  migrateDBJobsService,
		Value: strconv.FormatBool(dbmj.Data.MigrateDBJobsService),
	})
	if dbmj.Data.MigrateDBJobsService {
		envVars = append(envVars,
			corev1.EnvVar{
				Name:  quarkusDataSourceJobsServiceJdbcURL,
				Value: jsQuarkusDataSource.JdbcUrl,
			},
			corev1.EnvVar{
				Name: quarkusDataSourceJobsServiceUserName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  jsQuarkusDataSource.SecretUserKey,
						LocalObjectReference: jsDBSecretRef,
					},
				},
			},
			corev1.EnvVar{
				Name: quarkusDataSourceJobsServicePassword,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  jsQuarkusDataSource.SecretPasswordKey,
						LocalObjectReference: jsDBSecretRef,
					},
				},
			},
			corev1.EnvVar{
				Name:  quarkusFlywayJobsServiceSchemas,
				Value: jsQuarkusDataSource.Schema,
			})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dbMigrationJobCfg.JobName,
			Namespace: platform.Namespace,
			Labels:    lbl,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  dbMigrationJobCfg.ContainerName,
							Image: dbMigrationJobCfg.ToolImageName,
							Env:   envVars,
						},
					},
					RestartPolicy: "Never",
				},
			},
			BackoffLimit: pointer.Int32(0),
		},
	}
	return job
}

// GetDBMigrationJobStatus Returns db migration job status
func GetDBMigrationJobStatus(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, name string) (*DBMigratorJobStatus, error) {
	job, err := client.BatchV1().Jobs(platform.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		klog.V(log.E).InfoS("Error getting DB migrator job while monitoring completion: ", "error", err, "namespace", platform.Namespace, "job", name)
		return nil, err
	}
	return &DBMigratorJobStatus{name, &job.Status}, nil
}

// NewSonataFlowPlatformDBMigrationPhase Returns a new DB migration phase for SonataFlowPlatform
func NewSonataFlowPlatformDBMigrationPhase(status operatorapi.DBMigrationStatus, message string, reason string) *operatorapi.SonataFlowPlatformDBMigrationPhase {
	return &operatorapi.SonataFlowPlatformDBMigrationPhase{
		Status:  status,
		Message: message,
		Reason:  reason,
	}
}

// UpdateSonataFlowPlatformDBMigrationPhase Updates a given SonataFlowPlatformDBMigrationPhase with the supplied values
func UpdateSonataFlowPlatformDBMigrationPhase(dbMigrationStatus *operatorapi.SonataFlowPlatformDBMigrationPhase, status operatorapi.DBMigrationStatus, message string, reason string) *operatorapi.SonataFlowPlatformDBMigrationPhase {
	if dbMigrationStatus != nil {
		dbMigrationStatus.Status = status
		dbMigrationStatus.Message = message
		dbMigrationStatus.Reason = reason
		return dbMigrationStatus
	}
	return nil
}

func getKogitoDBMigratorToolImageName() string {

	imgTag := cfg.GetCfg().DbMigratorToolImageTag

	if imgTag == "" {
		// returns "docker.io/apache/incubator-kie-kogito-db-migrator-tool:<tag>"
		imgTag = fmt.Sprintf("%s-%s:%s", constants.ImageNamePrefix, constants.KogitoDBMigratorTool, version.GetImageTagVersion())
	}
	return imgTag
}

func newDBMigrationJobCfg(dbmjName string) *DBMigrationJobCfg {
	return &DBMigrationJobCfg{
		JobName:       dbmjName,
		ContainerName: dbMigrationContainerName,
		ToolImageName: getKogitoDBMigratorToolImageName(),
	}
}

func hasFailed(dbMigratorJobStatus *DBMigratorJobStatus) bool {
	return dbMigratorJobStatus.BatchJobStatus.Failed == dbMigrationJobFailed
}

func hasSucceeded(dbMigratorJobStatus *DBMigratorJobStatus) bool {
	return dbMigratorJobStatus.BatchJobStatus.Succeeded == dbMigrationJobSucceeded
}

// ReconcileDBMigrationJob Check the status of running DB migration job and return status
func ReconcileDBMigrationJob(ctx context.Context, client client.Client, platform *operatorapi.SonataFlowPlatform, name string) (*DBMigratorJobStatus, error) {
	platform.Status.SonataFlowPlatformDBMigrationPhase = NewSonataFlowPlatformDBMigrationPhase(operatorapi.DBMigrationStatusStarted, operatorapi.MessageDBMigrationStatusStarted, operatorapi.ReasonDBMigrationStatusStarted)

	dbMigratorJobStatus, err := GetDBMigrationJobStatus(ctx, client, platform, name)
	if err != nil {
		return nil, err
	}

	klog.V(log.I).InfoS("Db migration job status: ", "namespace", platform.Namespace, "job", dbMigratorJobStatus.Name, "active", dbMigratorJobStatus.BatchJobStatus.Active, "ready", dbMigratorJobStatus.BatchJobStatus.Ready, "failed", dbMigratorJobStatus.BatchJobStatus.Failed, "success", dbMigratorJobStatus.BatchJobStatus.Succeeded, "CompletedIndexes", dbMigratorJobStatus.BatchJobStatus.CompletedIndexes, "terminatedPods", dbMigratorJobStatus.BatchJobStatus.UncountedTerminatedPods)

	if hasFailed(dbMigratorJobStatus) {
		platform.Status.SonataFlowPlatformDBMigrationPhase = UpdateSonataFlowPlatformDBMigrationPhase(platform.Status.SonataFlowPlatformDBMigrationPhase, operatorapi.DBMigrationStatusFailed, operatorapi.MessageDBMigrationStatusFailed, operatorapi.ReasonDBMigrationStatusFailed)
		klog.V(log.I).InfoS("DB migration job failed", "namespace", platform.Namespace, "job", dbMigratorJobStatus.Name)
		return dbMigratorJobStatus, errors.New("DB migration job failed. namespace=" + platform.Namespace + " job=" + dbMigratorJobStatus.Name)
	} else if hasSucceeded(dbMigratorJobStatus) {
		platform.Status.SonataFlowPlatformDBMigrationPhase = UpdateSonataFlowPlatformDBMigrationPhase(platform.Status.SonataFlowPlatformDBMigrationPhase, operatorapi.DBMigrationStatusSucceeded, operatorapi.MessageDBMigrationStatusSucceeded, operatorapi.ReasonDBMigrationStatusSucceeded)
		klog.V(log.I).InfoS("DB migration job succeeded", "namespace", platform.Namespace, "job", dbMigratorJobStatus.Name)
	} else {
		// DB migration is still running
		platform.Status.SonataFlowPlatformDBMigrationPhase = UpdateSonataFlowPlatformDBMigrationPhase(platform.Status.SonataFlowPlatformDBMigrationPhase, operatorapi.DBMigrationStatusInProgress, operatorapi.MessageDBMigrationStatusInProgress, operatorapi.ReasonDBMigrationStatusInProgress)
	}

	return dbMigratorJobStatus, nil
}

func findRunningMigratorJobInNamespace(ctx context.Context, cli client.Client, namespace string) (*batchv1.Job, error) {
	jobList, err := findRunningMigratorJobsInNamespace(ctx, cli, namespace)
	if err != nil {
		return nil, err
	}
	if len(jobList.Items) > 0 {
		return &jobList.Items[0], nil
	}
	return nil, nil
}

func findRunningMigratorJobsInNamespace(ctx context.Context, cli client.Client, namespace string) (*batchv1.JobList, error) {
	jobList := &batchv1.JobList{}
	items := make([]batchv1.Job, 0)
	jobsInNamespace, err := kubernetes.FindJobs(ctx, cli, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to find jobs in namespace %s, %v", namespace, err)
	}
	for _, job := range jobsInNamespace.Items {
		if strings.HasPrefix(job.Name, dbMigrationJobName) {
			if finished, _ := kubernetes.JobHasFinished(&job); !finished {
				items = append(items, job)
			}
		}
	}
	jobList.Items = items
	return jobList, nil
}
