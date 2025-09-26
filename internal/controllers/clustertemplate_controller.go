/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
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
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	ctlrutils "github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	"gopkg.in/yaml.v3"
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
				slog.String("error", err.Error()))
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

	// Validate the timeout value from the hardware template if it's present
	if t.object.Spec.Templates.HwTemplate != "" {
		_, err = ctlrutils.GetTimeoutFromHWTemplate(ctx, t.client, t.object.Spec.Templates.HwTemplate)
		if err != nil {
			validationErrs = append(validationErrs, err.Error())
		}
	}

	// Validate the ClusterInstance defaults configmap
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.ClusterInstanceDefaults,
		t.object.Namespace,
		ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey,
		ctlrutils.ClusterInstallationTimeoutConfigKey)
	if err != nil {
		if !ctlrutils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for ClusterInstance defaults: %w",
				t.object.Spec.Templates.ClusterInstanceDefaults, err)
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
			if !ctlrutils.IsInputError(err) {
				return false, fmt.Errorf("failed to validate ClusterImageSet matches release: %w", err)
			}
			validationErrs = append(validationErrs, err.Error())
		}
	}

	// Validation for the policy template defaults configmap.
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.PolicyTemplateDefaults,
		t.object.Namespace,
		ctlrutils.PolicyTemplateDefaultsConfigmapKey,
		ctlrutils.ClusterConfigurationTimeoutConfigKey)
	if err != nil {
		if !ctlrutils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for policy template defaults: %w",
				t.object.Spec.Templates.PolicyTemplateDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validation for upgrade defaults confimap
	if t.object.Spec.Templates.UpgradeDefaults != "" {
		err = t.validateUpgradeDefaultsConfigmap(
			ctx, t.client, t.object.Spec.Templates.UpgradeDefaults,
			t.object.Namespace,
		)
		if err != nil {
			if !ctlrutils.IsInputError(err) {
				return false, fmt.Errorf("failed to validate the ConfigMap %s for upgrade defaults: %w",
					t.object.Spec.Templates.UpgradeDefaults, err)
			}
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

func (t *clusterTemplateReconcilerTask) validateUpgradeDefaultsConfigmap(
	ctx context.Context, c client.Client, name, namespace string,
) error {

	ibgu, err := ctlrutils.GetIBGUFromUpgradeDefaultsConfigmap(ctx, c, name, namespace, ctlrutils.UpgradeDefaultsConfigmapKey, "name", "name", namespace)
	if err != nil {
		return fmt.Errorf("failed to get IBGU from upgrade defaults configmap: %w", err)
	}

	if t.object.Spec.Release != ibgu.Spec.IBUSpec.SeedImageRef.Version {
		return ctlrutils.NewInputError(
			"The ClusterTemplate spec.release (%s) does not match the seedImageRef version (%s) from the upgrade configmap",
			t.object.Spec.Release, ibgu.Spec.IBUSpec.SeedImageRef.Version)
	}

	// Verify IBGU CR with dry-run
	opts := []client.CreateOption{}
	opts = append(opts, client.DryRunAll)
	err = c.Create(ctx, ibgu, opts...)
	if err != nil {
		if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
			return fmt.Errorf("failed to create IBGU: %w", err)
		}
		return ctlrutils.NewInputError("%s", err.Error())
	}
	existingConfigmap, err := ctlrutils.GetConfigmap(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}
	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return ctlrutils.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
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
			return ctlrutils.NewInputError("failed to validate the default ConfigMap: %w", err)
		}

		if err = ctlrutils.ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, c, data); err != nil {
			return ctlrutils.NewInputError("failed to validate the default ConfigMap: %w", err)
		}
	}

	// Extract and validate the timeout from the configmap
	_, err = ctlrutils.ExtractTimeoutFromConfigMap(existingConfigmap, timeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to validate timeout config: %w", err)
	}

	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return ctlrutils.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
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
		return ctlrutils.NewInputError("failed to validate ClusterTemplate name %s, should be in the format <spec.name>.<spec.version>: %s", metadataName, name+"."+version)
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
		return ctlrutils.NewInputError("failed to validate ClusterTemplate name %s, a identical name already exists in namespaces: %s",
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
			return ctlrutils.NewInputError("failed to validate templateID, invalid UUID:%s", object.Spec.TemplateID)
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
	mandatoryParams := [][]string{{ctlrutils.TemplateParamNodeClusterName, stringString},
		{ctlrutils.TemplateParamOCloudSiteId, stringString},
		{ctlrutils.TemplateParamPolicyConfig, objectString},
		{ctlrutils.TemplateParamClusterInstance, objectString}}
	if object.Spec.TemplateParameterSchema.Size() == 0 {
		return ctlrutils.NewInputError("templateParameterSchema is present but empty:")
	}
	var missingParameter []string
	var badType []string
	var subSchemas = make(map[string]any)
	for _, param := range mandatoryParams {
		expectedName := param[0]
		expectedType := param[1]
		aSubschema, err := provisioningv1alpha1.ExtractSubSchema(object.Spec.TemplateParameterSchema.Raw, expectedName)
		if err != nil {
			if strings.HasPrefix(err.Error(), fmt.Sprintf("subSchema '%s' does not exist:", expectedName)) {
				missingParameter = append(missingParameter, expectedName)
				continue
			} else {
				return fmt.Errorf("error extracting subschema at key %s: %w", expectedName, err)
			}
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
		return ctlrutils.NewInputError("%s", validationFailureReason)
	}
	if len(missingRequired) != 0 {
		validationFailureReason += fmt.Sprintf(" The following entries are missing in the required section of the template: %s",
			strings.Join(missingRequired, ","))
		return ctlrutils.NewInputError("%s", validationFailureReason)
	}

	policyTemplateParamsSchema := subSchemas[ctlrutils.TemplateParamPolicyConfig].(map[string]any)
	if err := validatePolicyTemplateParamsSchema(policyTemplateParamsSchema); err != nil {
		return ctlrutils.NewInputError("Error validating the policyTemplateParameters schema: %s", err.Error())
	}
	clusterInstanceParamsSchema := subSchemas[ctlrutils.TemplateParamClusterInstance].(map[string]any)
	if err := validateClusterInstanceParamsSchema(object.Spec.Templates.HwTemplate, clusterInstanceParamsSchema); err != nil {
		return ctlrutils.NewInputError("Error validating the clusterInstanceParameters schema: %s", err.Error())
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
		return fmt.Errorf("unexpected %s structure, no properties present", ctlrutils.TemplateParamPolicyConfig)
	}

	properties, isMap := propertiesInterface.(map[string]any)
	if !isMap {
		return fmt.Errorf("unexpected %s properties structure", ctlrutils.TemplateParamPolicyConfig)
	}

	for propertyKey, propertyValue := range properties {
		propertyValueMap, ok := propertyValue.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected %s structure for the %s property", ctlrutils.TemplateParamPolicyConfig, propertyKey)
		}

		valueTypeInterface, ok := propertyValueMap["type"]
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected subproperty \"type\" missing", ctlrutils.TemplateParamPolicyConfig)
		}

		valueType, ok := valueTypeInterface.(string)
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected the subproperty \"type\" to be string", ctlrutils.TemplateParamPolicyConfig)
		}

		if valueType != "string" {
			return fmt.Errorf("expected type string for the %s property", propertyKey)
		}
	}

	return nil
}

// validateClusterInstanceParamsSchema validates the cluster instance parameters schema.
func validateClusterInstanceParamsSchema(hwTemplate string, schema map[string]any) error {
	if hwTemplate == "" {
		return validateSchemaWithoutHWTemplate(schema)
	}
	return nil
}

// validateSchemaWithoutHWTemplate checks if the schema contains the expected properties
// when hardware template is not provided.
func validateSchemaWithoutHWTemplate(schema map[string]any) error {
	var expectedSubSchema map[string]any
	err := yaml.Unmarshal([]byte(ctlrutils.ClusterInstanceParamsSubSchemaForNoHWTemplate), &expectedSubSchema)
	if err != nil {
		return fmt.Errorf("failed to parse expected clusterInstanceParams subschema for no hwTemplate: %w", err)
	}

	if err := checkSchemaContains(schema, expectedSubSchema, ctlrutils.TemplateParamClusterInstance); err != nil {
		return fmt.Errorf("unexpected %s structure: %w", ctlrutils.TemplateParamClusterInstance, err)
	}

	return nil
}

// checkSchemaContains verifies that the actual schema contains all elements of the expected schema
func checkSchemaContains(actual, expected map[string]any, currentPath string) error {
	for key, expectedValue := range expected {
		actualValue, exists := actual[key]
		fullKey := currentPath + "." + key

		if !exists {
			return fmt.Errorf("missing key \"%s\" in field \"%s\"", key, currentPath)
		}

		switch expectedValue := expectedValue.(type) {
		case map[string]any:
			actualValueMap, ok := actualValue.(map[string]any)
			if !ok {
				return fmt.Errorf("expected a map for key \"%s\" in field \"%s\"", key, currentPath)
			}
			if err := checkSchemaContains(actualValueMap, expectedValue, fullKey); err != nil {
				return err
			}
		case []any:
			actualValueSlice, ok := actualValue.([]any)
			if !ok {
				return fmt.Errorf("expected a list for key \"%s\" in field \"%s\"", key, currentPath)
			}
			for _, item := range expectedValue {
				if !slices.Contains(actualValueSlice, item) {
					return fmt.Errorf("list in field \"%s\" is missing element: %v", fullKey, item)
				}
			}
		default:
			if actualValue != expectedValue {
				return fmt.Errorf("unexpected value for key \"%s\" in field \"%s\", expected: %v, actual: %v", key, currentPath, expectedValue, actualValue)
			}
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
			if clusterTemplate.Spec.Templates.ClusterInstanceDefaults == obj.GetName() ||
				clusterTemplate.Spec.Templates.PolicyTemplateDefaults == obj.GetName() ||
				clusterTemplate.Spec.Templates.UpgradeDefaults == obj.GetName() {
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
		ctx, t.client, t.object.Spec.Templates.ClusterInstanceDefaults, t.object.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ClusterInstanceDefaults ConfigMap %s: %w",
			t.object.Spec.Templates.ClusterInstanceDefaults, err)
	}

	// Extract the clusterinstance-defaults data from the ConfigMap
	clusterInstanceData, err := ctlrutils.ExtractTemplateDataFromConfigMap[map[string]any](
		clusterInstanceDefaultsCm, ctlrutils.ClusterInstanceTemplateDefaultsConfigmapKey)
	if err != nil {
		return fmt.Errorf("failed to extract clusterinstance-defaults from ConfigMap %s: %w",
			t.object.Spec.Templates.ClusterInstanceDefaults, err)
	}

	// Extract the clusterImageSetNameRef from the cluster instance data
	clusterImageSetNameRef, exists := clusterInstanceData["clusterImageSetNameRef"]
	if !exists {
		return ctlrutils.NewInputError(
			"clusterImageSetNameRef not found in ClusterInstanceDefaults ConfigMap %s",
			t.object.Spec.Templates.ClusterInstanceDefaults)
	}

	clusterImageSetName, ok := clusterImageSetNameRef.(string)
	if !ok {
		return ctlrutils.NewInputError(
			"clusterImageSetNameRef in ClusterInstanceDefaults ConfigMap %s is not a string: %T",
			t.object.Spec.Templates.ClusterInstanceDefaults, clusterImageSetNameRef)
	}

	// Fetch the ClusterImageSet resource
	clusterImageSet := &hivev1.ClusterImageSet{}
	err = t.client.Get(ctx, client.ObjectKey{Name: clusterImageSetName}, clusterImageSet)
	if err != nil {
		return fmt.Errorf("failed to get ClusterImageSet %s: %w", clusterImageSetName, err)
	}

	// Extract the release image from the ClusterImageSet spec
	releaseImage := clusterImageSet.Spec.ReleaseImage
	if releaseImage == "" {
		return fmt.Errorf("releaseImage not found in ClusterImageSet %s spec", clusterImageSetName)
	}

	// Extract version from the release image URL
	// Release image URLs typically contain the version, e.g.,
	// "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64"
	imageVersion := extractVersionFromReleaseImage(releaseImage)
	if imageVersion == "" {
		return fmt.Errorf("could not extract version from ClusterImageSet %s release image: %s",
			clusterImageSetName, releaseImage)
	}

	// Compare with the ClusterTemplate release version
	expectedVersion := t.object.Spec.Release
	if err := validateVersionsMatch(imageVersion, expectedVersion); err != nil {
		return ctlrutils.NewInputError(
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

// extractVersionFromReleaseImage extracts the OpenShift version from a release image URL
// Examples:
//   - "quay.io/openshift-release-dev/ocp-release:4.17.0-x86_64" -> "4.17.0"
//   - "registry.redhat.io/ubi8/ubi:4.16.1" -> "4.16.1"
func extractVersionFromReleaseImage(releaseImage string) string {
	// Split by ':' to get the tag part
	parts := strings.Split(releaseImage, ":")
	if len(parts) < 2 {
		return ""
	}

	tag := parts[len(parts)-1]

	// Look for version pattern (X.Y.Z with optional pre-release) in the tag
	// This regex matches semantic version patterns, excluding architecture suffixes
	// Architecture patterns like -x86_64, -aarch64 are NOT part of semver
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
