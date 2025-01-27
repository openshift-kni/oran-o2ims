package serviceconfig

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	api "github.com/openshift-kni/oran-o2ims/internal/service/alarms/api/generated"
	"github.com/openshift-kni/oran-o2ims/internal/service/alarms/internal/db/models"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/clients/k8s"
	serverutils "github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/dm"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	cleanupCronJobName   = "alarms-server-events-cleanup"
	cleanupConfigMapName = "alarms-server-events-cleanup-sql"
	cleanScriptDir       = "/scripts"
	cleanScriptName      = "cleanup.sql"
)

var (
	cronSchedule          = "0 * * * *" // run at the start of every hour e.g 2:00, 3:00 etc
	jobBackoffLimit int32 = 3
	successLimit    int32 = 3
	failureLimit    int32 = 1
	commonLabels          = map[string]string{
		"app": utils.InventoryAlarmServerName,
	}
)

// Config defines the configuration for serviceconfig
type Config struct {
	PostgresImage string        `envconfig:"POSTGRES_IMAGE" required:"true"` // PG image to use psql from
	PodNamespace  string        `envconfig:"POD_NAMESPACE" required:"true"`  // Dynamically check the current ns
	PgConnConfig  db.PgConfig   // Postgres config
	HubClient     client.Client // HubClient to manage cronjob resources
}

func LoadEnvConfig() (Config, error) {
	var config Config
	if err := envconfig.Process("", &config); err != nil {
		return config, err //nolint:wrapcheck
	}

	return config, nil
}

// EnsureCleanupCronJob starts (or updates) a cronjob with all the required resources to do alarms events cleanup.
func (c *Config) EnsureCleanupCronJob(ctx context.Context, sc *models.ServiceConfiguration) error {
	// Get the deployment alarms deployment and take ownership of resources
	deployment := &appsv1.Deployment{}
	if err := c.HubClient.Get(ctx, client.ObjectKey{
		Namespace: c.PodNamespace,
		Name:      utils.InventoryAlarmServerName,
	}, deployment); err != nil {
		return fmt.Errorf("failed to get alarms-server deployment: %w", err)
	}

	// Create CM with sql
	configMap, err := c.generateConfigMapWithSql(sc)
	if err != nil {
		return fmt.Errorf("generate sql configmap: %w", err)
	}
	if err := controllerutil.SetControllerReference(deployment, &configMap, c.HubClient.Scheme(),
		controllerutil.WithBlockOwnerDeletion(false)); err != nil {
		return fmt.Errorf("failed to set owner reference for configMap: %w", err)
	}
	if err := k8s.CreateOrUpdate(ctx, c.HubClient, &configMap); err != nil {
		return fmt.Errorf("failed to apply configmap: %w", err)
	}

	// Create CronJob that reads the CM
	cronJob := c.generateCronJob(configMap)
	if err := controllerutil.SetControllerReference(deployment, &cronJob, c.HubClient.Scheme(),
		controllerutil.WithBlockOwnerDeletion(false)); err != nil {
		return fmt.Errorf("failed to set owner reference for cronjob: %w", err)
	}
	if err := k8s.CreateOrUpdate(ctx, c.HubClient, &cronJob); err != nil {
		return fmt.Errorf("failed to apply cronjob: %w", err)
	}

	slog.Info("Successfully ensured cleanup cronjob and associated resources",
		"cronjob", cronJob.Name, "configmap", configMap.Name)
	return nil
}

// generateConfigMapWithSql a simple configMap to hold the sql command that is called from cronjob
func (c *Config) generateConfigMapWithSql(sc *models.ServiceConfiguration) (corev1.ConfigMap, error) {
	sql, err := getCleanUpPgSQL(sc)
	if err != nil {
		return corev1.ConfigMap{}, fmt.Errorf("failed to generate cleanup sql for configmap: %w", err)
	}

	return corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cleanupConfigMapName,
			Namespace: c.PodNamespace,
			Labels:    commonLabels,
		},
		Data: map[string]string{cleanScriptName: sql},
	}, nil
}

// getCleanUpPgSQL returns sql string to do alarms events cleanup based on service config
func getCleanUpPgSQL(sc *models.ServiceConfiguration) (string, error) {
	slog.Info("Using service config to generating sql", "serviceConfig", sc)

	// This should be checked earlier as well but double-checking here since sql is sensitive to it
	// A "0" basically means delete everything before now()
	if sc.RetentionPeriod <= 0 {
		return "", fmt.Errorf("invalid retention period: %d", sc.RetentionPeriod)
	}

	// Currently not supported
	if len(sc.Extensions) != 0 {
		slog.Warn("Service configuration extension is currently ignored")
	}

	aer := models.AlarmEventRecord{}
	dbTag := serverutils.GetAllDBTagsFromStruct(aer)
	query := psql.Delete(
		dm.From(aer.TableName()),
		dm.Where(
			psql.Quote(dbTag["AlarmClearedTime"]).LT(
				psql.Raw("now() - interval '"+strconv.Itoa(sc.RetentionPeriod)+" days'"),
			),
		),
		dm.Where(
			psql.Quote(dbTag["AlarmStatus"]).EQ(psql.S(string(api.Resolved))),
		),
	)

	sql, _, err := query.Build()
	if err != nil {
		return "", fmt.Errorf("failed to build query for alarms events cleanup: %w", err)
	}

	return sql, nil
}

// generateCronJob generates a new CR for cronjob that use our current PG to run psql commands
func (c *Config) generateCronJob(configMap corev1.ConfigMap) batchv1.CronJob {
	pgEnv := []corev1.EnvVar{
		{Name: "PGHOST", Value: c.PgConnConfig.Host},
		{Name: "PGPORT", Value: c.PgConnConfig.Port},
		{Name: "PGDATABASE", Value: c.PgConnConfig.Database},
		{Name: "PGUSER", Value: c.PgConnConfig.User},
		{
			Name: "PGPASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "postgres-server-passwords",
					},
					Key: utils.AlarmsPasswordEnvName,
				},
			},
		},
		{Name: "PGAPPNAME", Value: cleanupCronJobName},
		{Name: "PGCONNECT_TIMEOUT", Value: "10"},
	}

	return batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cleanupCronJobName,
			Namespace: c.PodNamespace,
			Labels:    commonLabels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   cronSchedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: ptr.To(successLimit),
			FailedJobsHistoryLimit:     ptr.To(failureLimit),
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: ptr.To(jobBackoffLimit),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:    cleanupCronJobName,
									Image:   c.PostgresImage,
									Command: []string{"psql"},
									Args:    []string{"-f", fmt.Sprintf("%s/%s", cleanScriptDir, cleanScriptName)},
									Env:     pgEnv,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("50m"),
											corev1.ResourceMemory: resource.MustParse("64Mi"),
										},
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      configMap.GetName(),
											MountPath: cleanScriptDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{{
								Name: configMap.GetName(),
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: configMap.GetName(),
										},
									},
								},
							}},
						},
					},
				},
			},
		},
	}
}
