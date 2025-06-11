/*
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"log/slog"
	"time"

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
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
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

func requeueImmediately() ctrl.Result {
	return ctrl.Result{Requeue: true}
}

func requeueWithCustomInterval(interval time.Duration) ctrl.Result {
	return ctrl.Result{RequeueAfter: interval}
}

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates/finalizers,verbs=update

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
	result = doNotRequeue()

	// Wait a bit before getting the object to allow it to be updated to
	// its current version and avoid older version during updates
	time.Sleep(100 * time.Millisecond)
	// Fetch the object:
	object := &provisioningv1alpha1.ClusterTemplate{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The cluster template could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ClusterTemplate",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile Cluster Template] "+object.Name)

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
	if t.object.Spec.TemplateID == "" {
		err := generateTemplateID(ctx, t.client, t.object)
		if err != nil {
			return requeueWithError(err)
		}
		return requeueImmediately(), nil
	}

	valid, err := t.validateClusterTemplateCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate ClusterTemplate",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		return requeueWithError(err)
	}
	if !valid {
		// Requeue for invalid clustertemplate
		return requeueWithLongInterval(), nil
	}

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
		_, err = utils.GetTimeoutFromHWTemplate(ctx, t.client, t.object.Spec.Templates.HwTemplate)
		if err != nil {
			validationErrs = append(validationErrs, err.Error())
		}
	}

	// Validate the ClusterInstance defaults configmap
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.ClusterInstanceDefaults,
		t.object.Namespace,
		utils.ClusterInstanceTemplateDefaultsConfigmapKey,
		utils.ClusterInstallationTimeoutConfigKey)
	if err != nil {
		if !utils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for ClusterInstance defaults: %w",
				t.object.Spec.Templates.ClusterInstanceDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validation for the policy template defaults configmap.
	err = validateConfigmapReference[map[string]any](
		ctx, t.client,
		t.object.Spec.Templates.PolicyTemplateDefaults,
		t.object.Namespace,
		utils.PolicyTemplateDefaultsConfigmapKey,
		utils.ClusterConfigurationTimeoutConfigKey)
	if err != nil {
		if !utils.IsInputError(err) {
			return false, fmt.Errorf("failed to validate the ConfigMap %s for policy template defaults: %w",
				t.object.Spec.Templates.PolicyTemplateDefaults, err)
		}
		validationErrs = append(validationErrs, err.Error())
	}

	// Validation for upgrade defaults confimap
	if t.object.Spec.Templates.UpgradeDefaults != "" {
		err = t.validateUpgradeDefaultsConfigmap(
			ctx, t.client, t.object.Spec.Templates.UpgradeDefaults,
			t.object.Namespace, utils.UpgradeDefaultsConfigmapKey,
		)
		if err != nil {
			if !utils.IsInputError(err) {
				return false, fmt.Errorf("failed to validate the ConfigMap %s for upgrade defaults: %w",
					t.object.Spec.Templates.UpgradeDefaults, err)
			}
			validationErrs = append(validationErrs, err.Error())
		}
	}

	validationErrsMsg := strings.Join(validationErrs, ";")
	if validationErrsMsg != "" {
		t.logger.ErrorContext(ctx, fmt.Sprintf(
			"Failed to validate for ClusterTemplate %s: %s", t.object.Name, validationErrsMsg))
	} else {
		t.logger.InfoContext(ctx, fmt.Sprintf(
			"Validation passing for ClusterTemplate %s", t.object.Name))
	}

	err = t.updateStatusConditionValidated(ctx, validationErrsMsg)
	if err != nil {
		return false, err
	}
	return validationErrsMsg == "", nil
}

func (t *clusterTemplateReconcilerTask) validateUpgradeDefaultsConfigmap(
	ctx context.Context, c client.Client, name, namespace, key string,
) error {

	ibgu, err := utils.GetIBGUFromUpgradeDefaultsConfigmap(ctx, c, name, namespace, key, "name", "name", namespace)
	if err != nil {
		return fmt.Errorf("failed to get IBGU from upgrade defaults configmap: %w", err)
	}

	if t.object.Spec.Release != ibgu.Spec.IBUSpec.SeedImageRef.Version {
		return utils.NewInputError(
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
		return utils.NewInputError("%s", err.Error())
	}
	existingConfigmap, err := utils.GetConfigmap(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}
	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return utils.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
	} else if existingConfigmap.Immutable == nil {
		// Patch the validated ConfigMap to make it immutable if not already set
		immutable := true
		newConfigmap := existingConfigmap.DeepCopy()
		newConfigmap.Immutable = &immutable

		if err := utils.CreateK8sCR(ctx, c, newConfigmap, nil, utils.PATCH); err != nil {
			return fmt.Errorf("failed to patch ConfigMap as immutable: %w", err)
		}
	}
	return nil
}

// validateConfigmapReference validates a given configmap reference within the ClusterTemplate
func validateConfigmapReference[T any](
	ctx context.Context, c client.Client, name, namespace, templateDataKey, timeoutConfigKey string) error {

	existingConfigmap, err := utils.GetConfigmap(ctx, c, name, namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigmapReference: %w", err)
	}

	// Extract and validate the template from the configmap
	data, err := utils.ExtractTemplateDataFromConfigMap[T](existingConfigmap, templateDataKey)
	if err != nil {
		return err
	}

	if templateDataKey == utils.ClusterInstanceTemplateDefaultsConfigmapKey {
		if err = utils.ValidateDefaultInterfaces(data); err != nil {
			return utils.NewInputError("failed to validate the default ConfigMap: %w", err)
		}

		if err = utils.ValidateConfigmapSchemaAgainstClusterInstanceCRD(ctx, c, data); err != nil {
			return utils.NewInputError("failed to validate the default ConfigMap: %w", err)
		}
	}

	// Extract and validate the timeout from the configmap
	_, err = utils.ExtractTimeoutFromConfigMap(existingConfigmap, timeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to validate timeout config: %w", err)
	}

	// Check if the configmap is set to mutable
	if existingConfigmap.Immutable != nil && !*existingConfigmap.Immutable {
		return utils.NewInputError("It is not allowed to set Immutable to false in the ConfigMap %s", name)
	} else if existingConfigmap.Immutable == nil {
		// Patch the validated ConfigMap to make it immutable if not already set
		immutable := true
		newConfigmap := existingConfigmap.DeepCopy()
		newConfigmap.Immutable = &immutable

		if err := utils.CreateK8sCR(ctx, c, newConfigmap, nil, utils.PATCH); err != nil {
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
		return utils.NewInputError("failed to validate ClusterTemplate name %s, should be in the format <spec.name>.<spec.version>: %s", metadataName, name+"."+version)
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
		return utils.NewInputError("failed to validate ClusterTemplate name %s, a identical name already exists in namespaces: %s",
			metadataName, strings.Join(utils.MapKeysToSlice(sameMetadataName), ","))
	}
	return nil
}

// validateTemplateID return true if the templateID is a valid uuid, false otherwise
// If the templateID does not exist, it is generated by this function using uuid
func validateTemplateID(object *provisioningv1alpha1.ClusterTemplate) error {
	if object.Spec.TemplateID != "" {
		_, err := uuid.Parse(object.Spec.TemplateID)
		if err != nil {
			return utils.NewInputError("failed to validate templateID, invalid UUID:%s", object.Spec.TemplateID)
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

	err := utils.CreateK8sCR(ctx, c, newTemplate, nil, utils.PATCH)
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
	mandatoryParams := [][]string{{utils.TemplateParamNodeClusterName, stringString},
		{utils.TemplateParamOCloudSiteId, stringString},
		{utils.TemplateParamPolicyConfig, objectString},
		{utils.TemplateParamClusterInstance, objectString}}
	if object.Spec.TemplateParameterSchema.Size() == 0 {
		return utils.NewInputError("templateParameterSchema is present but empty:")
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
	requiredList, err := utils.ExtractSchemaRequired(object.Spec.TemplateParameterSchema.Raw)
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
		return utils.NewInputError("%s", validationFailureReason)
	}
	if len(missingRequired) != 0 {
		validationFailureReason += fmt.Sprintf(" The following entries are missing in the required section of the template: %s",
			strings.Join(missingRequired, ","))
		return utils.NewInputError("%s", validationFailureReason)
	}

	policyTemplateParamsSchema := subSchemas[utils.TemplateParamPolicyConfig].(map[string]any)
	if err := validatePolicyTemplateParamsSchema(policyTemplateParamsSchema); err != nil {
		return utils.NewInputError("Error validating the policyTemplateParameters schema: %s", err.Error())
	}
	clusterInstanceParamsSchema := subSchemas[utils.TemplateParamClusterInstance].(map[string]any)
	if err := validateClusterInstanceParamsSchema(object.Spec.Templates.HwTemplate, clusterInstanceParamsSchema); err != nil {
		return utils.NewInputError("Error validating the clusterInstanceParameters schema: %s", err.Error())
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
		return fmt.Errorf("unexpected %s structure, no properties present", utils.TemplateParamPolicyConfig)
	}

	properties, isMap := propertiesInterface.(map[string]any)
	if !isMap {
		return fmt.Errorf("unexpected %s properties structure", utils.TemplateParamPolicyConfig)
	}

	for propertyKey, propertyValue := range properties {
		propertyValueMap, ok := propertyValue.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected %s structure for the %s property", utils.TemplateParamPolicyConfig, propertyKey)
		}

		valueTypeInterface, ok := propertyValueMap["type"]
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected subproperty \"type\" missing", utils.TemplateParamPolicyConfig)
		}

		valueType, ok := valueTypeInterface.(string)
		if !ok {
			return fmt.Errorf("unexpected %s structure: expected the subproperty \"type\" to be string", utils.TemplateParamPolicyConfig)
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
	err := yaml.Unmarshal([]byte(utils.ClusterInstanceParamsSubSchemaForNoHWTemplate), &expectedSubSchema)
	if err != nil {
		return fmt.Errorf("failed to parse expected clusterInstanceParams subschema for no hwTemplate: %w", err)
	}

	if err := checkSchemaContains(schema, expectedSubSchema, utils.TemplateParamClusterInstance); err != nil {
		return fmt.Errorf("unexpected %s structure: %w", utils.TemplateParamClusterInstance, err)
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
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.CTconditionTypes.Validated,
			provisioningv1alpha1.CTconditionReasons.Failed,
			metav1.ConditionFalse,
			errMsg,
		)
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			provisioningv1alpha1.CTconditionTypes.Validated,
			provisioningv1alpha1.CTconditionReasons.Completed,
			metav1.ConditionTrue,
			"The cluster template validation succeeded",
		)
	}

	err := utils.UpdateK8sCRStatus(ctx, t.client, t.object)
	if err != nil {
		return fmt.Errorf("failed to update status for ClusterTemplate %s: %w", t.object.Name, err)
	}
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
		r.Logger.Error("Unable to list ClusterTemplate resources. ", "error: ", err.Error())
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
