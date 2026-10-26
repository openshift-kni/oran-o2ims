/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const seedGenerationISOUploadCleanupTimeout = 5 * time.Minute

// Called only after all ISO Jobs and Pods have disappeared. The frozen upload
// credentials must remain available until cleanup completes or its budget ends.
func (t *provisioningRequestReconcilerTask) cleanupSeedGenerationISOUpload(ctx context.Context) (bool, error) {
	details := t.object.Status.Extensions.ClusterDetails
	if details == nil || details.SeedGenerationStatus == nil {
		return false, nil
	}
	status := details.SeedGenerationStatus
	if !status.ISOJobSubmitted || status.ISOUploadCleanupCompleted || t.seedGenerationISOCompleted() {
		return false, nil
	}
	if status.ISOUploadCleanupDeadline == nil {
		deadline := metav1.NewTime(time.Now().Add(seedGenerationISOUploadCleanupTimeout))
		status.ISOUploadCleanupDeadline = &deadline
		if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
			return false, fmt.Errorf("failed to record ISO upload cleanup deadline: %w", err)
		}
	}
	if !time.Now().Before(status.ISOUploadCleanupDeadline.Time) {
		return false, t.completeSeedGenerationISOUploadCleanup(ctx, true)
	}
	snapshot, err := t.loadSeedGenerationInputSnapshot(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Waiting for frozen ISO upload cleanup inputs", slog.Any("error", err))
		return true, nil
	}
	liveISO, _ := snapshot.Document.SeedGeneration["liveISO"].(map[string]any)
	upload := snapshot.Secrets["upload"]
	artifact, err := seedGenerationISOArtifactForRun(t.object, liveISO, upload)
	if err != nil {
		return false, t.completeSeedGenerationISOUploadCleanup(ctx, true)
	}
	command := seedGenerationISOUploadCleanupCommand(artifact, string(t.object.UID))
	output, err := runSeedGenerationSSHCommand(ctx, string(upload.Data["host"]), upload.Data, command)
	if err != nil {
		slog.WarnContext(ctx, "Retrying incomplete ISO upload cleanup", slog.Any("error", err))
		return true, nil
	}
	return false, t.completeSeedGenerationISOUploadCleanup(ctx, strings.TrimSpace(string(output)) != "removed")
}

func seedGenerationISOUploadCleanupCommand(artifact *seedGenerationISOArtifact, uid string) string {
	directory := artifact.RemoteDirectory
	owner := path.Join(directory, seedGenerationISOUploadOwnerFile)
	final := path.Join(directory, "rhcos-ibi.iso")
	// Do not recursively remove anything: a pre-existing directory or a marker
	// belonging to another uploader must never authorize deletion of its files.
	return fmt.Sprintf("set -eu; if test ! -e '%s'; then echo removed; "+
		"elif test -f '%s' && test \"$(cat '%s')\" = '%s'; then "+
		"rm -f -- '%s.tmp-%s' '%s' '%s'; rmdir -- '%s'; echo removed; "+
		"elif rmdir -- '%s' 2>/dev/null; then echo removed; else echo unowned; fi",
		directory, owner, owner, uid, final, uid, final, owner, directory, directory)
}

func (t *provisioningRequestReconcilerTask) completeSeedGenerationISOUploadCleanup(ctx context.Context, manual bool) error {
	status := t.object.Status.Extensions.ClusterDetails.SeedGenerationStatus
	if manual {
		message := "Incomplete live ISO upload could not be safely removed; check the upload server's seedgen/" +
			string(t.object.UID) + " directory and remove this run's files manually"
		slog.WarnContext(ctx, message)
		if condition := seedGenerationCondition(t.object); condition != nil && !strings.Contains(condition.Message, message) {
			condition.Message += "; " + message
		}
		if t.object.Status.ProvisioningStatus.ProvisioningPhase == provisioningv1alpha1.StateFailed {
			t.object.Status.ProvisioningStatus.ProvisioningDetails += "; " + message
		}
	}
	status.ISOUploadCleanupCompleted = true
	if err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to record ISO upload cleanup completion: %w", err)
	}
	return nil
}
