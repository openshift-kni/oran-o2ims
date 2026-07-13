/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"log/slog"
	"time"

	"github.com/coreos/go-semver/semver"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/google/uuid"
	hwmgmtv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/constants"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	typederrors "github.com/openshift-kni/oran-o2ims/internal/typed-errors"
	upgradevalidation "github.com/openshift-kni/oran-o2ims/internal/validation"
)

// ClusterTemplateReconciler reconciles a ClusterTemplate object
type ClusterTemplateReconciler struct {
	client.Client
	Logger *slog.Logger
}

type clusterTemplateReconcilerTask struct {
	logger *slog.Logger
	client client.Client
	object *provisioningv1alpha1.ClusterTemplate
}

func doNotRequeue() ctrl.Result {
	return ctrl.Result{Requeue: false}
}

func requeueWithError(err error) (ctrl.Result, error) {
	// can not be fixed by user during reconcile
	return ctrl.Result{}, err
}

func requeueWithLongInterval() ctrl.Result {
	return requeueWithCustomInterval(5 * time.Minute)
}

func requeueWithMediumInterval() ctrl.Result {
	return requeueWithCustomInterval(1 * time.Minute)
}

func requeueWithShortInterval() ctrl.Result {
	return requeueWithCustomInterval(15 * time.Second)
}

func requeueImmediately() ctrl.Result {
	return ctrl.Result{Requeue: true}
}

func requeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=clustertemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=clustertemplates/finalizers,verbs=update
//+kubebuilder:rbac:groups=clcm.openshift.io,resources=hardwareprofiles,verbs=get;list;watch
//+kubebuilder:rbac:groups=hive.openshift.io,resources=clusterimagesets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClusterTemplate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile

func (r *ClusterTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (
	result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	startTime := time.Now()
	result = doNotRequeue()

	// Add standard reconciliation context
	ctx = ctlrutils.LogReconcileStart(ctx, r.Logger, req, "ClusterTemplate")

	defer func() {
		duration := time.Since(startTime)
		if err != nil {
			r.Logger.ErrorContext(ctx, "Reconciliation failed",
				slog.Duration("duration", duration),
				slog.Any("error", err))
		} else {
			r.Logger.InfoContext(ctx, "Reconciliation completed",
				slog.Duration("duration", duration),
				slog.Bool("requeue", result.Requeue),
				slog.Duration("requeueAfter", result.RequeueAfter))
		}
	}()

	// Wait a bit before getting the object to allow it to be updated to
	// its current version and avoid older version during updates
	time.Sleep(100 * time.Millisecond)
	// Fetch the object:
	object := &provisioningv1alpha1.ClusterTemplate{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The cluster template could have been deleted
			r.Logger.InfoContext(ctx, "ClusterTemplate not found, assuming deleted")
			err = nil
			return
		}
		ctlrutils.LogError(ctx, r.Logger, "Unable to fetch ClusterTemplate", err)
		return
	}

	// Add object-specific context
	ctx = ctlrutils.AddObjectContext(ctx, object)
	r.Logger.InfoContext(ctx, "Fetched ClusterTemplate successfully")

	// Create and run the task:
	task := &clusterTemplateReconcilerTask{
		logger: r.Logger,
		client: r.Client,
		object: object,
	}
	result, err = task.run(ctx)
	return
}

func (t *clusterTemplateReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	startTime := time.Now()

	if t.object.Spec.TemplateID == "" {
		ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "generate_template_id")
		err := generateTemplateID(ctx, t.client, t.object)
		if err != nil {
			ctlrutils.LogError(ctx, t.logger, "Failed to generate template ID", err)
			return requeueWithError(err)
		}
		ctlrutils.LogPhaseComplete(ctx, t.logger, "generate_template_id", time.Since(startTime))
		return requeueImmediately(), nil
	}

	ctx = ctlrutils.LogPhaseStart(ctx, t.logger, "validation")
	valid, err := t.validateClusterTemplateCR(ctx)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to validate ClusterTemplate", err,
			slog.String("name", t.object.Name))
		return requeueWithError(err)
	}
	if !valid {
		// Requeue for invalid clustertemplate
		t.logger.InfoContext(ctx, "ClusterTemplate validation failed, requeueing")
		return requeueWithLongInterval(), nil
	}

	ctlrutils.LogPhaseComplete(ctx, t.logger, "validation", time.Since(startTime))
	t.logger.InfoContext(ctx, "ClusterTemplate reconciliation completed successfully")
	return doNotRequeue(), nil
}

// validateClusterTemplateCR validates the ClusterTemplate CR and updates the Validated status condition.
// It returns true if valid, false otherwise.
func (t *clusterTemplateReconcilerTask) validateClusterTemplateCR(ctx context.Context) (bool, error) {
	var validationErrs []string

	// Validate the ClusterInstance name
	err := validateName(t.client, t.object.Spec.Name, t.object.Spec.Version, t.object.Name, t.object.Namespace)
	if err != nil {
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate the Template ID
	err = validateTemplateID(t.object)
	if err != nil {
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate templateParameterSchema field
	err = validateTemplateParameterSchema(t.object)
	if err != nil {
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate the hwMgmtDefaults inline data
	hwMgmtErrs, err := t.validateHwMgmtDefaults(ctx)
	if err != nil {
		return false, err
	}
	validationErrs = append(validationErrs, hwMgmtErrs...)

	// Validate the ClusterInstance defaults configmap
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.TemplateDefaults.ClusterInstanceDefaults,
		t.object.Namespace,
		ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
		ctlrutils.ClusterInstallationTimeoutConfigKey)
	if err != nil {
		if !typederrors.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for ClusterInstance defaults: %w",
				t.object.Spec.TemplateDefaults.ClusterInstanceDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate that ClusterImageSet matches release version
	skipValidationValue, hasSkipAnnotation := t.object.Annotations[ctlrutils.SkipClusterImageSetValidationAnnotation]
	shouldSkipClusterImageSetValidation := hasSkipAnnotation && strings.EqualFold(skipValidationValue, "true")

	if shouldSkipClusterImageSetValidation {
		t.logger.InfoContext(ctx, "Skipping ClusterImageSet validation due to annotation",
			slog.String("name", t.object.Name),
			slog.String("annotation", ctlrutils.SkipClusterImageSetValidationAnnotation))
	} else {
		t.logger.InfoContext(ctx, "Validating ClusterImageSet", slog.String("name", t.object.Name))
		err = t.validateClusterImageSetMatchesRelease(ctx)
		if err != nil {
			if !typederrors.IsInputError(err) {
				return false, fmt.Errorf("failed to validate ClusterImageSet matches release: %w", err)
			}
			validationErrs = append(validationErrs, err.Error())
		}
	}

	// Validation for the policy template defaults configmap.
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.TemplateDefaults.PolicyTemplateDefaults,
		t.object.Namespace,
		ctlrutils.PolicyTemplateDefaultsConfigmapKey,
		ctlrutils.ClusterConfigurationTimeoutConfigKey)
	if err != nil {
		if !typederrors.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for policy template defaults: %w",
				t.object.Spec.TemplateDefaults.PolicyTemplateDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validate spec.release is valid semver
	if t.object.Spec.Release != "" {
		if _, err = semver.NewVersion(t.object.Spec.Release); err != nil {
			validationErrs = append(validationErrs,
				fmt.Sprintf("spec.release %q is not a valid semantic version: %s",
					t.object.Spec.Release, err.Error()))
		}
	}

	// Validation for upgrade defaults
	if t.object.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0 {
		err = t.validateUpgradeDefaults()
		if err != nil {
			validationErrs = append(validationErrs, err.Error())
		}
	}

	validationErrsMsg := strings.Join(validationErrs, ";")
	if validationErrsMsg != "" {
		t.logger.ErrorContext(ctx, "ClusterTemplate validation failed",
			slog.String("name", t.object.Name),
			slog.String("validationErrors", validationErrsMsg))
	} else {
		t.logger.InfoContext(ctx, "ClusterTemplate validation passed",
			slog.String("name", t.object.Name))
	}

	err = t.updateStatusConditionValidated(ctx, validationErrsMsg)
	if err != nil {
		return false, err
	}
	return validationErrsMsg == "", nil
}

// validateHwMgmtDefaults validates the hwMgmtDefaults inline data in the ClusterTemplate.
// Returns validation error messages and a fatal error if a transient failure occurs.
func (t *clusterTemplateReconcilerTask) validateHwMgmtDefaults(ctx context.Context) ([]string, error) {
	var validationErrs []string

	if len(t.object.Spec.TemplateDefaults.HwMgmtDefaults.NodeGroupData) > 0 {
		seenNodeGroups := map[string]struct{}{}
		seenRoles := map[string]string{}
		hwProfileNS := ctlrutils.GetEnvOrDefault(constants.DefaultNamespaceEnvName, constants.DefaultNamespace)
		for _, ng := range t.object.Spec.TemplateDefaults.HwMgmtDefaults.NodeGroupData {
			if _, exists := seenNodeGroups[ng.Name]; exists {
				validationErrs = append(validationErrs,
					fmt.Sprintf("duplicate nodeGroupData name %q in hwMgmtDefaults", ng.Name))
			}
			seenNodeGroups[ng.Name] = struct{}{}
			if prev, exists := seenRoles[ng.Role]; exists {
				validationErrs = append(validationErrs,
					fmt.Sprintf("duplicate role %q in hwMgmtDefaults for groups %q and %q", ng.Role, prev, ng.Name))
			}
			seenRoles[ng.Role] = ng.Name
			if ng.HwProfile != "" {
				hwProfileObj := &hwmgmtv1alpha1.HardwareProfile{}
				if err := t.client.Get(ctx, client.ObjectKey{Name: ng.HwProfile, Namespace: hwProfileNS}, hwProfileObj); err != nil {
					if errors.IsNotFound(err) {
						validationErrs = append(validationErrs,
							fmt.Sprintf("HardwareProfile %q referenced by nodeGroup %q does not exist", ng.HwProfile, ng.Name))
					} else {
						return nil, fmt.Errorf("failed to get HardwareProfile %q for nodeGroup %q: %w", ng.HwProfile, ng.Name, err)
					}
				}
			}
		}
	}

	// Validate hardwareProvisioningTimeout independently of nodeGroupData
	if t.object.Spec.TemplateDefaults.HwMgmtDefaults.HardwareProvisioningTimeout != nil {
		if t.object.Spec.TemplateDefaults.HwMgmtDefaults.HardwareProvisioningTimeout.Duration <= 0 {
			validationErrs = append(validationErrs,
				fmt.Sprintf("hardwareProvisioningTimeout %q must be a positive duration",
					t.object.Spec.TemplateDefaults.HwMgmtDefaults.HardwareProvisioningTimeout.Duration))
		}
	}

	return validationErrs, nil
}

func (t *clusterTemplateReconcilerTask) validateUpgradeDefaults() error {
	var upgradeData map[string]any
	if err := json.Unmarshal(t.object.Spec.TemplateDefaults.UpgradeDefaults.Raw, &upgradeData); err != nil {
		return fmt.Errorf("upgradeDefaults is not a map: %w", err)
	}

	hasCV := schemaPropertyExists(upgradeData, ctlrutils.UpgradeDefaultsClusterVersionKey)
	hasIBGU := schemaPropertyExists(upgradeData, ctlrutils.UpgradeDefaultsIBGUKey)
	if hasCV && hasIBGU {
		return typederrors.NewInputError(
			"upgradeDefaults contains both %q and %q keys; only one upgrade type is allowed",
			ctlrutils.UpgradeDefaultsClusterVersionKey, ctlrutils.UpgradeDefaultsIBGUKey)
	}

	if hasCV || hasIBGU {
		if err := t.validateUpgradeDefaultsAgainstSchema(upgradeData, hasCV, hasIBGU); err != nil {
			return err
		}
	}

	if hasCV {
		if err := t.validateCVUpgradeDefaults(upgradeData); err != nil {
			return err
		}
	} else if hasIBGU {
		if err := t.validateIBGUUpgradeDefaults(); err != nil {
			return err
		}
	}

	return nil
}

func (t *clusterTemplateReconcilerTask) validateCVUpgradeDefaults(upgradeData map[string]any) error {
	if err := upgradevalidation.ValidateCVUpgradeData(upgradeData, t.object.Spec.Release, "upgradeDefaults"); err != nil {
		return fmt.Errorf("clusterVersion upgrade validation failed: %w", err)
	}
	return nil
}

func (t *clusterTemplateReconcilerTask) validateIBGUUpgradeDefaults() error {
	var upgradeDataRaw map[string]json.RawMessage
	if err := json.Unmarshal(t.object.Spec.TemplateDefaults.UpgradeDefaults.Raw, &upgradeDataRaw); err != nil {
		return fmt.Errorf("upgradeDefaults is not a map: %w", err)
	}
	ibguData, ok := upgradeDataRaw[ctlrutils.UpgradeDefaultsIBGUKey]
	if !ok {
		return nil
	}

	ibgu, err := ctlrutils.GetIBGUFromUpgradeData(ibguData, "name", "name", t.object.Namespace)
	if err != nil {
		return typederrors.NewInputError("failed to get IBGU from upgradeDefaults: %s", err.Error())
	}

	if ibgu.Spec.IBUSpec.SeedImageRef.Version != "" && t.object.Spec.Release != ibgu.Spec.IBUSpec.SeedImageRef.Version {
		return typederrors.NewInputError(
			"The ClusterTemplate spec.release (%s) does not match the seedImageRef version (%s) from the upgrade defaults",
			t.object.Spec.Release, ibgu.Spec.IBUSpec.SeedImageRef.Version)
	}

	return nil
}

// validateUpgradeDefaultsAgainstSchema verifies that the upgradeDefaults upgrade type
// matches the type defined in the upgradeParameters schema and that defaults conform
// to that schema.
func (t *clusterTemplateReconcilerTask) validateUpgradeDefaultsAgainstSchema(
	upgradeData map[string]any, defaultsHasCV, defaultsHasIBGU bool) error {

	if t.object.Spec.TemplateParameterSchema.Size() == 0 {
		return typederrors.NewInputError(
			"templateParameterSchema must define %q when upgradeDefaults is set",
			constants.TemplateParamUpgrade)
	}

	upgradeSchema, err := provisioningv1alpha1.ExtractSubSchema(
		t.object.Spec.TemplateParameterSchema.Raw, constants.TemplateParamUpgrade)
	if err != nil {
		if provisioningv1alpha1.IsErrSubSchemaNotFound(err) {
			return typederrors.NewInputError(
				"templateParameterSchema must define %q when upgradeDefaults is set",
				constants.TemplateParamUpgrade)
		}
		return fmt.Errorf("failed to extract %q schema: %w", constants.TemplateParamUpgrade, err)
	}

	props, ok := upgradeSchema["properties"].(map[string]any)
	if !ok {
		return typederrors.NewInputError(
			"%q schema must have a properties section",
			constants.TemplateParamUpgrade)
	}

	schemaHasCV := schemaPropertyExists(props, ctlrutils.UpgradeDefaultsClusterVersionKey)
	schemaHasIBGU := schemaPropertyExists(props, ctlrutils.UpgradeDefaultsIBGUKey)

	if defaultsHasCV && !schemaHasCV {
		return typederrors.NewInputError(
			"upgradeDefaults defines %q, but %s schema does not include a matching %q definition",
			ctlrutils.UpgradeDefaultsClusterVersionKey, constants.TemplateParamUpgrade, ctlrutils.UpgradeDefaultsClusterVersionKey)
	}
	if defaultsHasIBGU && !schemaHasIBGU {
		return typederrors.NewInputError(
			"upgradeDefaults defines %q, but %s schema does not include a matching %q definition",
			ctlrutils.UpgradeDefaultsIBGUKey, constants.TemplateParamUpgrade, ctlrutils.UpgradeDefaultsIBGUKey)
	}

	if err := provisioningv1alpha1.ValidateJsonAgainstJsonSchema(upgradeSchema, upgradeData); err != nil {
		return typederrors.NewInputError(
			"upgradeDefaults does not conform to the %s schema: %s",
			constants.TemplateParamUpgrade, err.Error())
	}

	return nil
}

// validateConfigmapReference validates a given configmap reference within the ClusterTemplate
func validateConfigmapReference[T any](
	ctx context.Context, c client.Client, name, namespace, templateDataKey, timeoutConfigKey string) error {

	existingConfigmap, err := ctlrutils.GetConfigmap(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}

	// Extract and validate the template from the configmap
	data, err := ctlrutils.ExtractTemplateDataFromConfigMap[T](existingConfigmap, templateDataKey)
	if err != nil {
		return err
	}

	if templateDataKey == ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey {
		if err = ctlrutils.ValidateDefaultInterfaces(data); err != nil {
			return typederrors.NewInputError("failed to validate the default ConfigMap: %w", err)
		}

		if err = ctlrutils.ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, c, data); err != nil {
			return typederrors.NewInputError("failed to validate the default ConfigMap: %w", err)
		}
	}

	// Extract and validate the timeout from the configmap
	_, err = ctlrutils.ExtractTimeoutFromConfigMap(existingConfigmap, timeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to validate timeout config: %w", err)
	}

	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return typederrors.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
	} else if existingConfigmap.Immutable == nil {
		// Patch the validated ConfigMap to make it immutable if not already set
		immutable := true
		newConfigmap := existingConfigmap.DeepCopy()
		newConfigmap.Immutable = &immutable

		if err := ctlrutils.CreateK8sCR(ctx, c, newConfigmap, nil, ctlrutils.PATCH); err != nil {
			return fmt.Errorf("failed to patch ConfigMap as immutable: %w", err)
		}
	}

	return nil
}

// validateName return true if the ClusterTemplate name is the
// format: <name>.<version> or if the cluster <name> is used in
// another namespace for a ClusterTemplate, false otherwise
func validateName(c client.Client, name, version, metadataName, namespace string) error {
	if metadataName != name+"."+version {
		return typederrors.NewInputError("failed to validate ClusterTemplate name %s, should be in the format <spec.name>.<spec.version>: %s", metadataName, name+"."+version)
	}

	allClusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}
	err := c.List(context.Background(), allClusterTemplates)
	if err != nil {
		return fmt.Errorf("could not get list of ClusterTemplate across the cluster: %w", err)
	}

	sameMetadataName := map[string]bool{}
	for _, aClusterTemplate := range allClusterTemplates.Items {
		if aClusterTemplate.Namespace == namespace {
			continue
		}
		if aClusterTemplate.Name == metadataName {
			sameMetadataName[aClusterTemplate.Namespace] = true
		}
	}
	if len(sameMetadataName) != 0 {
		return typederrors.NewInputError("failed to validate ClusterTemplate name %s, a identical name already exists in namespaces: %s",
			metadataName, strings.Join(ctlrutils.MapKeysToSlice(sameMetadataName), ","))
	}
	return nil
}

// validateTemplateID return true if the templateID is a valid uuid, false otherwise
// If the templateID does not exist, it is generated by this function using uuid
func validateTemplateID(object *provisioningv1alpha1.ClusterTemplate) error {
	if object.Spec.TemplateID != "" {
		_, err := uuid.Parse(object.Spec.TemplateID)
		if err != nil {
			return typederrors.NewInputError("failed to validate templateID, invalid UUID:%s", object.Spec.TemplateID)
		}
	}
	return nil
}

// generateTemplateID generates a new templateId if it was not present
// If the templateID does not exist, it is generated by this function using uuid
func generateTemplateID(ctx context.Context, c client.Client, object *provisioningv1alpha1.ClusterTemplate) error {
	if object.Spec.TemplateID != "" {
		return nil
	}
	newID := uuid.New()
	newTemplate := object.DeepCopy()
	newTemplate.Spec.TemplateID = newID.String()

	err := ctlrutils.CreateK8sCR(ctx, c, newTemplate, nil, ctlrutils.PATCH)
	if err != nil {
		return fmt.Errorf("failed to patch templateID in ClusterTemplate %s: %w", object.Name, err)
	}

	return nil
}

// validateTemplateParameterSchema return true if the schema contained in the templateParameterSchema
// field contains the required mandatory parameters
// - nodeClusterName
// - oCloudSiteId
// - policyTemplateParameters
// - clusterInstanceParameters
func validateTemplateParameterSchema(object *provisioningv1alpha1.ClusterTemplate) error {
	const (
		typeString   = "type"
		stringString = "string"
		objectString = "object"
	)
	mandatoryParams := [][]string{{constants.TemplateParamNodeClusterName, stringString},
		{constants.TemplateParamOCloudSiteId, stringString},
		{constants.TemplateParamPolicyConfig, objectString},
		{constants.TemplateParamClusterInstance, objectString}}
	if object.Spec.TemplateParameterSchema.Size() == 0 {
		return typederrors.NewInputError("templateParameterSchema is present but empty:")
	}
	var missingParameter []string
	var badType []string
	var subSchemas = make(map[string]any)
	for _, param := range mandatoryParams {
		expectedName := param[0]
		expectedType := param[1]
		aSubschema, err := provisioningv1alpha1.ExtractSubSchema(object.Spec.TemplateParameterSchema.Raw, expectedName)
		if err != nil {
			if provisioningv1alpha1.IsErrSubSchemaNotFound(err) {
				missingParameter = append(missingParameter, expectedName)
				continue
			}
			return fmt.Errorf("error extracting subschema at key %s: %w", expectedName, err)
		}
		if aType, ok := aSubschema[typeString]; ok {
			if aType != expectedType {
				badType = append(badType, fmt.Sprintf("%s (expected = %s actual= %s)", expectedName, expectedType, aType))
			}
		} else {
			badType = append(badType, fmt.Sprintf("%s (expected = %s actual= none)", expectedName, expectedType))
		}
		subSchemas[expectedName] = aSubschema
	}
	var missingRequired []string
	requiredList, err := ctlrutils.ExtractSchemaRequired(object.Spec.TemplateParameterSchema.Raw)
	if err != nil {
		return fmt.Errorf("error unmarshalling required subschema: %w", err)
	}
	for _, param := range mandatoryParams {
		expectedName := param[0]
		if !slices.Contains(requiredList, expectedName) {
			missingRequired = append(missingRequired, expectedName)
		}
	}
	validationFailureReason := fmt.Sprintf("failed to validate ClusterTemplate: %s.", object.Name)
	if len(missingParameter) != 0 {
		validationFailureReason += fmt.Sprintf(" The following mandatory fields are missing: %s.", strings.Join(missingParameter, ","))
	}
	if len(badType) != 0 {
		validationFailureReason += fmt.Sprintf(" The following entries are present but have a unexpected type: %s.",
			strings.Join(badType, ","))
		return typederrors.NewInputError("%s", validationFailureReason)
	}
	if len(missingRequired) != 0 {
		validationFailureReason += fmt.Sprintf(" The following entries are missing in the required section of the template: %s",
			strings.Join(missingRequired, ","))
		return typederrors.NewInputError("%s", validationFailureReason)
	}

	policyTemplateParamsSchema := subSchemas[constants.TemplateParamPolicyConfig].(map[string]any)
	if err := validatePolicyTemplateParamsSchema(policyTemplateParamsSchema); err != nil {
		return typederrors.NewInputError("Error validating the policyTemplateParameters schema: %s", err.Error())
	}
	// Require hardware provisioning: the CT must have hwMgmtDefaults.nodeGroupData OR
	// the templateParameterSchema must expose hwMgmtParameters (allowing the PR to supply it).
	hasHwMgmt := len(object.Spec.TemplateDefaults.HwMgmtDefaults.NodeGroupData) > 0 ||
		provisioningv1alpha1.SchemaDefinesHwMgmtParameters(object)
	if !hasHwMgmt {
		return typederrors.NewInputError(
			"ClusterTemplate must define hardware provisioning via hwMgmtDefaults.nodeGroupData " +
				"or expose hwMgmtParameters in templateParameterSchema")
	}

	hasUpgradeDefaults := object.Spec.TemplateDefaults.UpgradeDefaults.Size() > 0
	if err := validateUpgradeParametersSchema(object.Spec.TemplateParameterSchema.Raw, hasUpgradeDefaults); err != nil {
		return typederrors.NewInputError("Error validating the %s schema: %s", constants.TemplateParamUpgrade, err.Error())
	}

	return nil
}

// validateUpgradeParametersSchema validates the upgradeParameters sub-schema structure.
// When present, it must have type "object" with a properties section containing
// exactly one of "clusterVersion" or "imageBasedGroupUpgrade" (both is rejected).
// When upgradeDefaults is set, the sub-schema is required.
func validateUpgradeParametersSchema(schemaRaw []byte, hasUpgradeDefaults bool) error {
	upgradeSchema, err := provisioningv1alpha1.ExtractSubSchema(schemaRaw, constants.TemplateParamUpgrade)
	if err != nil {
		if !provisioningv1alpha1.IsErrSubSchemaNotFound(err) {
			return fmt.Errorf("failed to extract %q schema: %w", constants.TemplateParamUpgrade, err)
		}
		if hasUpgradeDefaults {
			return fmt.Errorf("templateParameterSchema must define %q when upgradeDefaults is set", constants.TemplateParamUpgrade)
		}
		return nil
	}

	if t, _ := upgradeSchema["type"].(string); t != "object" {
		return fmt.Errorf("%q must have type \"object\"", constants.TemplateParamUpgrade)
	}
	props, ok := upgradeSchema["properties"].(map[string]any)
	if !ok {
		return fmt.Errorf("%q schema must have a properties section", constants.TemplateParamUpgrade)
	}

	hasCV := schemaPropertyExists(props, ctlrutils.UpgradeDefaultsClusterVersionKey)
	hasIBGU := schemaPropertyExists(props, ctlrutils.UpgradeDefaultsIBGUKey)

	if hasCV && hasIBGU {
		return fmt.Errorf("%q schema must not define both %q and %q; choose exactly one upgrade type",
			constants.TemplateParamUpgrade,
			ctlrutils.UpgradeDefaultsClusterVersionKey,
			ctlrutils.UpgradeDefaultsIBGUKey)
	}
	if hasUpgradeDefaults && !hasCV && !hasIBGU {
		return fmt.Errorf("%q schema must define either %q or %q when upgradeDefaults is set",
			constants.TemplateParamUpgrade,
			ctlrutils.UpgradeDefaultsClusterVersionKey,
			ctlrutils.UpgradeDefaultsIBGUKey)
	}

	if err := validateUpgradeTypeProperty(props, ctlrutils.UpgradeDefaultsClusterVersionKey); err != nil {
		return err
	}
	if err := validateUpgradeTypeProperty(props, ctlrutils.UpgradeDefaultsIBGUKey); err != nil {
		return err
	}

	return nil
}

func schemaPropertyExists(props map[string]any, key string) bool {
	_, ok := props[key]
	return ok
}

func validateUpgradeTypeProperty(props map[string]any, key string) error {
	prop, ok := props[key]
	if !ok {
		return nil
	}
	propMap, ok := prop.(map[string]any)
	if !ok {
		return fmt.Errorf("%s.%s must be an object schema",
			constants.TemplateParamUpgrade, key)
	}
	if t, _ := propMap["type"].(string); t != "object" {
		return fmt.Errorf("%s.%s must have type \"object\"",
			constants.TemplateParamUpgrade, key)
	}
	return nil
}

// validatePolicyTemplateParamsSchema ensure the policyTemplateParameters schema has the right format,
// where only one level properties with a string type are present.
// Example:
// policyTemplateParameters:
//
//	description: policyTemplateSchema defines the available parameters for cluster configuration
//	properties:
//	  cluster-log-fwd-filters:
//	    type: string
//	  cluster-log-fwd-outputs:
//	    type: string
//	  cluster-log-fwd-pipelines:
//	    type: string
func validatePolicyTemplateParamsSchema(schema map[string]any) error {
	propertiesInterface, hasProperties := schema["properties"]
	if !hasProperties {
		return fmt.Errorf("unexpected %s structure, no properties present", constants.TemplateParamPolicyConfig)
	}

	properties, isMap := propertiesInterface.(map[string]any)
	if !isMap {
		return fmt.Errorf("unexpected %s properties structure", constants.TemplateParamPolicyConfig)
	}

	for propertyKey, propertyValue := range properties {
		propertyValueMap, ok := propertyValue.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected %s structure for the %s property", constants.TemplateParamPolicyConfig, propertyKey)
		}

		valueTypeInterface, ok := propertyValueMap["type"]
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected subproperty \"type\" missing", constants.TemplateParamPolicyConfig)
		}

		valueType, ok := valueTypeInterface.(string)
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected the subproperty \"type\" to be string", constants.TemplateParamPolicyConfig)
		}

		if valueType != "string" {
			return fmt.Errorf("expected type string for the %s property", propertyKey)
		}
	}

	return nil
}

// setStatusConditionValidated updates the Validated status condition of the ClusterTemplate object
func (t *clusterTemplateReconcilerTask) updateStatusConditionValidated(ctx context.Context, errMsg string) error {
	if errMsg != "" {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.CTconditionTypes.Validated,
			provisioningv1alpha1.CTconditionReasons.Failed,
			metav1.ConditionFalse,
			errMsg,
		)
	} else {
		ctlrutils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.CTconditionTypes.Validated,
			provisioningv1alpha1.CTconditionReasons.Completed,
			metav1.ConditionTrue,
			"The cluster template validation succeeded",
		)
	}

	err := ctlrutils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		ctlrutils.LogError(ctx, t.logger, "Failed to update ClusterTemplate status", err,
			slog.String("name", t.object.Name))
		return fmt.Errorf("failed to update status for ClusterTemplate %s: %w", t.object.Name, err)
	}

	t.logger.InfoContext(ctx, "ClusterTemplate status updated successfully")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-template").
		For(&provisioningv1alpha1.ClusterTemplate{},
			// Watch for create and update events for ClusterTemplate.
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					oldGeneration := e.ObjectOld.GetGeneration()
					newGeneration := e.ObjectNew.GetGeneration()

					// Reconcile on spec update only
					return oldGeneration != newGeneration
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return true },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueClusterTemplatesForConfigmap),
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					cmOld := e.ObjectOld.(*corev1.ConfigMap)
					cmNew := e.ObjectNew.(*corev1.ConfigMap)

					// Reconcile on data update only
					return !equality.Semantic.DeepEqual(cmOld.Data, cmNew.Data)
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return true },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}

// enqueueClusterTemplatesForConfigmap identifies and enqueues ClusterTemplates that reference a given ConfigMap.
func (r *ClusterTemplateReconciler) enqueueClusterTemplatesForConfigmap(ctx context.Context, obj client.Object) []reconcile.Request {
	var requests []reconcile.Request

	// Get all the cluster templates
	clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}
	err := r.List(ctx, clusterTemplates)
	if err != nil {
		ctlrutils.LogError(ctx, r.Logger, "Unable to list ClusterTemplate resources", err)
		return nil
	}

	for _, clusterTemplate := range clusterTemplates.Items {
		if clusterTemplate.Namespace == obj.GetNamespace() {
			if clusterTemplate.Spec.TemplateDefaults.ClusterInstanceDefaults == obj.GetName() ||
				clusterTemplate.Spec.TemplateDefaults.PolicyTemplateDefaults == obj.GetName() {
				// The configmap is referenced in this cluster template , enqueue it
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: clusterTemplate.Namespace,
						Name:      clusterTemplate.Name,
					},
				})
			}
		}
	}
	return requests
}

// validateClusterImageSetMatchesRelease validates that the ClusterImageSet referenced in the
// ClusterTemplate.spec.templates.clusterInstanceDefaults matches the release version specified
// in the ClusterTemplate.spec.release
func (t *clusterTemplateReconcilerTask) validateClusterImageSetMatchesRelease(ctx context.Context) error {

	// Get the ClusterInstanceDefaults ConfigMap
	clusterInstanceDefaultsCm, err := ctlrutils.GetConfigmap(
		ctx, t.client, t.object.Spec.TemplateDefaults.ClusterInstanceDefaults, t.object.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ClusterInstanceDefaults ConfigMap %s: %w",
			t.object.Spec.TemplateDefaults.ClusterInstanceDefaults, err)
	}

	// Extract the clusterinstance-defaults data from the ConfigMap
	clusterInstanceData, err := ctlrutils.ExtractTemplateDataFromConfigMap[map[string]any](
		clusterInstanceDefaultsCm, ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		return fmt.Errorf("failed to extract clusterinstance-defaults from ConfigMap %s: %w",
			t.object.Spec.TemplateDefaults.ClusterInstanceDefaults, err)
	}

	// Extract the clusterImageSetNameRef from the cluster instance data
	clusterImageSetNameRef, exists := clusterInstanceData["clusterImageSetNameRef"]
	if !exists {
		return typederrors.NewInputError(
			"clusterImageSetNameRef not found in ClusterInstanceDefaults ConfigMap %s",
			t.object.Spec.TemplateDefaults.ClusterInstanceDefaults)
	}

	clusterImageSetName, ok := clusterImageSetNameRef.(string)
	if !ok {
		return typederrors.NewInputError(
			"clusterImageSetNameRef in ClusterInstanceDefaults ConfigMap %s is not a string: %T",
			t.object.Spec.TemplateDefaults.ClusterInstanceDefaults, clusterImageSetNameRef)
	}

	// Fetch the ClusterImageSet resource
	clusterImageSet := &hivev1.ClusterImageSet{}
	err = t.client.Get(ctx, client.ObjectKey{Name: clusterImageSetName}, clusterImageSet)
	if err != nil {
		return fmt.Errorf("failed to get ClusterImageSet %s: %w", clusterImageSetName, err)
	}

	// Extract the version from the ClusterImageSet. ACM 2.17+ adds a releaseTag
	// label (e.g., "4.20.4-x86_64") and uses digest references in releaseImage.
	// Older ACM versions use tagged image references (e.g., ":4.20.4-x86_64").
	imageVersion := extractVersionFromClusterImageSet(clusterImageSet)
	if imageVersion == "" {
		return fmt.Errorf("could not extract version from ClusterImageSet %s: no releaseTag label and release image %s has no parseable tag",
			clusterImageSetName, clusterImageSet.Spec.ReleaseImage)
	}

	// Compare with the ClusterTemplate release version
	expectedVersion := t.object.Spec.Release
	if err := validateVersionsMatch(imageVersion, expectedVersion); err != nil {
		return typederrors.NewInputError(
			"ClusterImageSet %s version (%s) does not match ClusterTemplate release version (%s): %w",
			clusterImageSetName, imageVersion, expectedVersion, err)
	}

	t.logger.InfoContext(ctx,
		"ClusterImageSet version matches ClusterTemplate release",
		slog.String("clusterImageSet", clusterImageSetName),
		slog.String("imageVersion", imageVersion),
		slog.String("expectedVersion", expectedVersion),
	)

	return nil
}

// extractVersionFromClusterImageSet extracts the OpenShift version from a ClusterImageSet.
// It first checks the releaseTag label (added by ACM 2.17+ alongside digest references),
// then falls back to parsing the version from a tagged release image reference.
func extractVersionFromClusterImageSet(cis *hivev1.ClusterImageSet) string {
	if tag, ok := cis.Labels["releaseTag"]; ok && tag != "" {
		if v := extractVersionFromTag(tag); v != "" {
			return v
		}
	}

	return extractVersionFromReleaseImage(cis.Spec.ReleaseImage)
}

// extractVersionFromReleaseImage extracts the OpenShift version from a tagged release image URL.
// Examples:
//   - "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64" -> "4.17.0"
//   - "quay.io/openshift-release-dev/ocp-release@sha256:abc123" -> "" (digest, no tag)
func extractVersionFromReleaseImage(releaseImage string) string {
	parts := strings.Split(releaseImage, ":")
	if len(parts) < 2 {
		return ""
	}
	return extractVersionFromTag(parts[len(parts)-1])
}

// extractVersionFromTag extracts a semantic version (X.Y.Z) from a tag string,
// ignoring architecture suffixes like -x86_64 or -aarch64.
func extractVersionFromTag(tag string) string {
	versionRegex := `(\d+\.\d+\.\d+(?:-(?:rc|alpha|beta)[0-9\.]*)*)`
	matches := regexp.MustCompile(versionRegex).FindStringSubmatch(tag)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// validateVersionsMatch compares two version strings using semantic versioning
// This function ensures both versions are valid semver and match exactly
func validateVersionsMatch(imageVersion, templateVersion string) error {
	// Normalize versions by removing 'v' prefix if present
	normalizedImageVersion := strings.TrimPrefix(imageVersion, "v")
	normalizedTemplateVersion := strings.TrimPrefix(templateVersion, "v")

	// Parse the image version using semver
	imageSemver, err := semver.NewVersion(normalizedImageVersion)
	if err != nil {
		return fmt.Errorf("failed to parse ClusterImageSet version '%s' as semver: %w", imageVersion, err)
	}

	// Parse the template version using semver
	templateSemver, err := semver.NewVersion(normalizedTemplateVersion)
	if err != nil {
		return fmt.Errorf("failed to parse ClusterTemplate release version '%s' as semver: %w", templateVersion, err)
	}

	// Compare versions for exact match
	if imageSemver.Compare(*templateSemver) != 0 {
		return fmt.Errorf("versions do not match exactly: ClusterImageSet version %s != ClusterTemplate version %s",
			imageSemver.String(), templateSemver.String())
	}

	return nil
}
