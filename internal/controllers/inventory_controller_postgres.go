/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sptr "k8s.io/utils/ptr"

	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/postgres"
)

// deployPostgresServer deploys the actual Postgres database server instance.  Prior to invoking this method the other
// required resources must have already been created (i.e., configmaps, secrets, service accounts, etc...).
func (t *reconcilerTask) deployPostgresServer(ctx context.Context, serverName string) error {
	t.logger.InfoContext(ctx, "[deploy postgres server]", "Name", serverName)

	// Default server volumes.
	deploymentVolumes := utils.GetDeploymentVolumes(serverName, t.object)
	deploymentVolumeMounts := utils.GetDeploymentVolumeMounts(serverName, t.object)

	// Add additional database volumes.
	deploymentVolumes = append(deploymentVolumes,
		corev1.Volume{
			Name: fmt.Sprintf("%s-config", serverName),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-config", serverName),
					},
				},
			},
		},
		corev1.Volume{
			Name: fmt.Sprintf("%s-startup", serverName),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: fmt.Sprintf("%s-startup", serverName),
					},
				},
			},
		},
	)

	deploymentVolumeMounts = append(deploymentVolumeMounts,
		corev1.VolumeMount{
			Name:      fmt.Sprintf("%s-config", serverName),
			MountPath: "/opt/app-root/src/postgresql-cfg",
		},
		corev1.VolumeMount{
			Name:      fmt.Sprintf("%s-startup", serverName),
			MountPath: "/opt/app-root/src/postgresql-start",
		})

	// Build the deployment's metadata.
	deploymentMeta := metav1.ObjectMeta{
		Name:      serverName,
		Namespace: t.object.Namespace,
		Labels: map[string]string{
			"oran/o2ims": t.object.Name,
			"app":        serverName,
		},
	}

	postgresImage := os.Getenv(utils.PostgresImageName)
	if postgresImage == "" {
		return fmt.Errorf("missing %s environment variable value", utils.PostgresImageName)
	}

	// Disable privilege escalation
	privilegeEscalation := false

	// Build the deployment's spec.
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: k8sptr.To(int32(1)),
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": serverName,
			},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"kubectl.kubernetes.io/default-container": utils.ServerContainerName,
				},
				Labels: map[string]string{
					"app": serverName,
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: serverName,
				Volumes:            deploymentVolumes,
				Containers: []corev1.Container{
					{
						Name: utils.ServerContainerName,
						EnvFrom: []corev1.EnvFromSource{
							{
								SecretRef: &corev1.SecretEnvSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: fmt.Sprintf("%s-passwords", serverName),
									},
								},
							},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &privilegeEscalation,
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{"ALL"},
							},
						},
						Image:           postgresImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports: []corev1.ContainerPort{
							{
								Name:          utils.DatabaseTargetPort,
								Protocol:      corev1.ProtocolTCP,
								ContainerPort: utils.DatabaseServicePort,
							},
						},
						VolumeMounts: deploymentVolumeMounts,
						Resources: corev1.ResourceRequirements{ // Values here are derived from current PG tuning (update postgresql.conf and then these values as needed)
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
				},
			},
		},
	}

	// Build the deployment.
	newDeployment := &appsv1.Deployment{
		ObjectMeta: deploymentMeta,
		Spec:       deploymentSpec,
	}

	t.logger.InfoContext(ctx, "[deployDatabase] Create/Update/Patch Server", "Name", serverName)
	if err := utils.CreateK8sCR(ctx, t.client, newDeployment, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to deploy database: %w", err)
	}

	return nil
}

// createPasswords sets up the admin and service passwords for the Postgres database.
func (t *reconcilerTask) createPasswords(ctx context.Context, serverName string) error {
	t.logger.InfoContext(ctx, "[createPasswords]")

	// Create the passwords secret
	passwordSecretName := fmt.Sprintf("%s-passwords", serverName)
	var existing corev1.Secret
	err := t.client.Get(ctx, types.NamespacedName{Name: passwordSecretName, Namespace: t.object.Namespace}, &existing)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to query for existing passwords: %w", err)
	}

	if errors.IsNotFound(err) {
		// Does not already exist; create it.
		err = utils.CreateSecretFromLiterals(ctx, t.client, t.object, t.object.Namespace, passwordSecretName, map[string][]byte{
			utils.AdminPasswordEnvName:     []byte(utils.GetPasswordOrRandom(utils.AdminPasswordEnvName)),
			utils.AlarmsPasswordEnvName:    []byte(utils.GetPasswordOrRandom(utils.AlarmsPasswordEnvName)),
			utils.ResourcesPasswordEnvName: []byte(utils.GetPasswordOrRandom(utils.ResourcesPasswordEnvName)),
			utils.ClustersPasswordEnvName:  []byte(utils.GetPasswordOrRandom(utils.ClustersPasswordEnvName)),
		})
		if err != nil {
			return fmt.Errorf("failed to create passwords: %w", err)
		}
	} else {
		// Password secret already exists; make sure that required passwords are present.
		// Only create these once to avoid resetting the passwords.  We could update these passwords, but we would need
		// to force a restart of the postgres service so that its init scripts run again.  Updating the secret without
		// restarting the postgres service will cause password authentication to fail for all the servers using the new
		// set of passwords.
		if _, ok := existing.Data[utils.AdminPasswordEnvName]; !ok {
			existing.Data[utils.AdminPasswordEnvName] = []byte(utils.GetPasswordOrRandom(utils.AdminPasswordEnvName))
		}
		if _, ok := existing.Data[utils.AlarmsPasswordEnvName]; !ok {
			existing.Data[utils.AlarmsPasswordEnvName] = []byte(utils.GetPasswordOrRandom(utils.AlarmsPasswordEnvName))
		}
		if _, ok := existing.Data[utils.ResourcesPasswordEnvName]; !ok {
			existing.Data[utils.ResourcesPasswordEnvName] = []byte(utils.GetPasswordOrRandom(utils.ResourcesPasswordEnvName))
		}
		if _, ok := existing.Data[utils.ClustersPasswordEnvName]; !ok {
			existing.Data[utils.ClustersPasswordEnvName] = []byte(utils.GetPasswordOrRandom(utils.ClustersPasswordEnvName))
		}

		err = utils.CreateK8sCR(ctx, t.client, &existing, t.object, utils.UPDATE)
		if err != nil {
			return fmt.Errorf("failed to create passwords: %w", err)
		}
	}

	return nil
}

// createDatabase sets up all necessary resources to instantiate a Postgres database server.
func (t *reconcilerTask) createDatabase(ctx context.Context) (err error) {
	t.logger.InfoContext(ctx, "[createDatabase]")

	err = t.createServiceAccount(ctx, utils.InventoryDatabaseServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy ServiceAccount for the database server.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.createService(ctx, utils.InventoryDatabaseServerName, utils.DatabaseServicePort, utils.DatabaseTargetPort, 0, "")
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy Service for database.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the config volume
	t.logger.InfoContext(ctx, "[createDatabase] creating database config volume")
	configVolumeName := fmt.Sprintf("%s-config", utils.InventoryDatabaseServerName)
	err = utils.CreateConfigMapFromEmbeddedFile(ctx, t.client, t.object,
		postgres.Artifacts, postgres.ConfigFilePath, t.object.Namespace, configVolumeName, postgres.ConfigFileName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to make config configmap for database.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the startup volume
	t.logger.InfoContext(ctx, "[createDatabase] creating database startup volume")
	startupVolumeName := fmt.Sprintf("%s-startup", utils.InventoryDatabaseServerName)
	err = utils.CreateConfigMapFromEmbeddedFile(ctx, t.client, t.object,
		postgres.Artifacts, postgres.StartupFilePath, t.object.Namespace, startupVolumeName, postgres.StartupFileName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create startup configmap for database.",
			slog.String("error", err.Error()),
		)
		return
	}

	// Create the service passwords
	t.logger.InfoContext(ctx, "[createDatabase] creating database service passwords")
	err = t.createPasswords(ctx, utils.InventoryDatabaseServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to create service passwords for database.",
			slog.String("error", err.Error()),
		)
		return
	}

	err = t.deployPostgresServer(ctx, utils.InventoryDatabaseServerName)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to deploy the database server.",
			slog.String("error", err.Error()),
		)
	}

	return
}
