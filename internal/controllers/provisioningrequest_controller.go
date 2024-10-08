/*
Copyright 2023.

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
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hwv1alpha1 "github.com/openshift-kni/oran-o2ims/api/hardwaremanagement/v1alpha1"
	provisioningv1alpha1 "github.com/openshift-kni/oran-o2ims/api/provisioning/v1alpha1"
	"github.com/openshift-kni/oran-o2ims/internal/controllers/utils"
	siteconfig "github.com/stolostron/siteconfig/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	policiesv1 "open-cluster-management.io/governance-policy-propagator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ProvisioningRequestReconciler reconciles a ProvisioningRequest object
type ProvisioningRequestReconciler struct {
	client.Client
	Logger *slog.Logger
}

type provisioningRequestReconcilerTask struct {
	logger       *slog.Logger
	client       client.Client
	object       *provisioningv1alpha1.ProvisioningRequest
	clusterInput *clusterInput
	ctNamespace  string
	timeouts     *timeouts
}

// clusterInput holds the merged input data for a cluster
type clusterInput struct {
	clusterInstanceData map[string]any
	policyTemplateData  map[string]any
}

type nodeInfo struct {
	bmcAddress     string
	bmcCredentials string
	nodeName       string
	interfaces     []*hwv1alpha1.Interface
}

// timeouts holds the timeout values, in minutes,
// for hardware provisioning, cluster provisioning
// and cluster configuration.
type timeouts struct {
	hardwareProvisioningMinutes int
	clusterProvisioningMinutes  int
	configurationMinutes        int
}

type deleteOrUpdateEvent interface {
	event.UpdateEvent | event.DeleteEvent
}

const (
	provisioningRequestFinalizer = "provisioningrequest.o2ims.provisioning.oran.org/finalizer"
	provisioningRequestNameLabel = "provisioningrequest.o2ims.provisioning.oran.org/name"
	ztpDoneLabel                 = "ztp-done"
)

func getClusterTemplateRefName(name, version string) string {
	return fmt.Sprintf("%s.%s", name, version)
}

//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=provisioningrequests/finalizers,verbs=update
//+kubebuilder:rbac:groups=o2ims.provisioning.oran.org,resources=clustertemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=siteconfig.open-cluster-management.io,resources=clusterinstances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodepools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=o2ims-hardwaremanagement.oran.openshift.io,resources=nodes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=policy.open-cluster-management.io,resources=policies,verbs=list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ProvisioningRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ProvisioningRequestReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	_ = log.FromContext(ctx)
	result = doNotRequeue()

	// Reconciliation loop can be triggered multiple times for the same resource
	// due to changes in related resources, events or conditions.
	// Wait a bit so that API server/etcd syncs up and this reconcile has a
	// better chance of getting the latest resources.
	time.Sleep(100 * time.Millisecond)

	// Fetch the object:
	object := &provisioningv1alpha1.ProvisioningRequest{}
	if err = r.Client.Get(ctx, req.NamespacedName, object); err != nil {
		if errors.IsNotFound(err) {
			// The provisioning request could have been deleted
			err = nil
			return
		}
		r.Logger.ErrorContext(
			ctx,
			"Unable to fetch ProvisioningRequest",
			slog.String("error", err.Error()),
		)
		return
	}

	r.Logger.InfoContext(ctx, "[Reconcile ProvisioningRequest]",
		"name", object.Name, "namespace", object.Namespace)

	if res, stop, err := r.handleFinalizer(ctx, object); !res.IsZero() || stop || err != nil {
		if err != nil {
			r.Logger.ErrorContext(
				ctx,
				"Encountered error while handling the ProvisioningRequest finalizer",
				slog.String("err", err.Error()))
		}
		return res, err
	}

	// Create and run the task:
	task := &provisioningRequestReconcilerTask{
		logger:       r.Logger,
		client:       r.Client,
		object:       object,
		clusterInput: &clusterInput{},
		ctNamespace:  "",
		timeouts:     &timeouts{},
	}
	result, err = task.run(ctx)
	return
}

func (t *provisioningRequestReconcilerTask) run(ctx context.Context) (ctrl.Result, error) {
	// Validate the ProvisioningRequest
	err := t.handleValidation(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		// internal error that might recover
		return requeueWithError(err)
	}

	// Render and validate ClusterInstance
	renderedClusterInstance, err := t.handleRenderClusterInstance(ctx)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		return requeueWithError(err)
	}

	// Handle the creation of resources required for cluster deployment
	err = t.handleClusterResources(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			_, err = t.checkClusterDeployConfigState(ctx)
			if err != nil {
				return requeueWithError(err)
			}
			// Requeue since we are not watching for updates to required resources
			// if they are missing
			return requeueWithMediumInterval(), nil
		}
		return requeueWithError(err)
	}

	// Render the hardware template for NodePool
	renderedNodePool, err := t.renderHardwareTemplate(ctx, renderedClusterInstance)
	if err != nil {
		if utils.IsInputError(err) {
			return t.checkClusterDeployConfigState(ctx)
		}
		return requeueWithError(err)
	}

	// Create/Update the NodePool
	err = t.createNodePoolResources(ctx, renderedNodePool)
	if err != nil {
		return requeueWithError(err)
	}

	// wait for the NodePool to be provisioned and update BMC details in ClusterInstance
	provisioned, timedOutOrFailed, err := t.waitForHardwareData(ctx, renderedClusterInstance, renderedNodePool)
	if err != nil {
		return requeueWithError(err)
	}
	if timedOutOrFailed {
		// Timeout occurred or failed, stop requeuing
		return doNotRequeue(), nil
	}
	if !provisioned {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Waiting for NodePool %s in the namespace %s to be provisioned",
				renderedNodePool.GetName(),
				renderedNodePool.GetNamespace(),
			),
		)
		// TODO: Remove this check once hwmgr plugin(s) are fully utilized
		if renderedNodePool.ObjectMeta.Namespace != utils.TempDellPluginNamespace && renderedNodePool.ObjectMeta.Namespace != utils.UnitTestHwmgrNamespace {
			return requeueWithMediumInterval(), nil
		}
	}

	hwProvisionedCond := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.PRconditionTypes.HardwareProvisioned))

	if hwProvisionedCond != nil {
		// TODO: check hwProvisionedCond.Status == metav1.ConditionTrue
		// after hw plugin is ready

		// Handle the cluster install with ClusterInstance
		err := t.handleClusterInstallation(ctx, renderedClusterInstance)
		if err != nil {
			return requeueWithError(err)
		}
	}

	// Handle policy configuration only after the cluster provisioning
	// has started, and not failed or timedout (completed, in-progress or unknown)
	if utils.IsClusterProvisionPresent(t.object) &&
		!utils.IsClusterProvisionTimedOutOrFailed(t.object) {

		// Handle configuration through policies.
		requeue, err := t.handleClusterPolicyConfiguration(ctx)
		if err != nil {
			return requeueWithError(err)
		}

		// Requeue if cluster provisioning is not completed (in-progress or unknown)
		// or there are enforce policies that are not Compliant
		if !utils.IsClusterProvisionCompleted(t.object) || requeue {
			return requeueWithLongInterval(), nil
		}
	}

	return doNotRequeue(), nil
}

// checkClusterDeployConfigState checks the current deployment and configuration state of
// the cluster by evaluating the statuses of related resources like NodePool, ClusterInstance
// and policy configuration when applicable, and update the corresponding ProvisioningRequest
// status conditions
func (t *provisioningRequestReconcilerTask) checkClusterDeployConfigState(ctx context.Context) (ctrl.Result, error) {
	// Check the NodePool status if exists
	if t.object.Status.NodePoolRef == nil {
		return doNotRequeue(), nil
	}
	nodePool := &hwv1alpha1.NodePool{}
	nodePool.SetName(t.object.Status.NodePoolRef.Name)
	nodePool.SetNamespace(t.object.Status.NodePoolRef.Namespace)
	provisioned, timedOutOrFailed, err := t.checkNodePoolProvisionStatus(ctx, nodePool)
	if err != nil {
		return requeueWithError(err)
	}
	if timedOutOrFailed {
		// Timeout occurred or failed, stop requeuing
		return doNotRequeue(), nil
	}
	// TODO: remove the namespace check once the hwmgr plugin are fully utilized
	if !provisioned && nodePool.Namespace != utils.TempDellPluginNamespace && nodePool.Namespace != utils.UnitTestHwmgrNamespace {
		return requeueWithMediumInterval(), nil
	}

	hwProvisionedCond := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.PRconditionTypes.HardwareProvisioned))
	if hwProvisionedCond != nil {
		// Check the ClusterInstance status if exists
		if t.object.Status.ClusterDetails == nil {
			return doNotRequeue(), nil
		}
		err := t.checkClusterProvisionStatus(
			ctx, t.object.Status.ClusterDetails.Name)
		if err != nil {
			return requeueWithError(err)
		}
	}

	// Check the policy configuration status only after the cluster provisioning
	// has started, and not failed or timedout
	if utils.IsClusterProvisionPresent(t.object) &&
		!utils.IsClusterProvisionTimedOutOrFailed(t.object) {
		requeue, err := t.handleClusterPolicyConfiguration(ctx)
		if err != nil {
			return requeueWithError(err)
		}
		// Requeue if Cluster Provisioned is not completed (in-progress or unknown)
		// or there are enforce policies that are not Compliant
		if !utils.IsClusterProvisionCompleted(t.object) || requeue {
			return requeueWithLongInterval(), nil
		}
	}
	return doNotRequeue(), nil
}

func (t *provisioningRequestReconcilerTask) handleValidation(ctx context.Context) error {
	// Validate provisioning request CR
	err := t.validateProvisioningRequestCR(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to validate the ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.Validated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to validate the ProvisioningRequest: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Validated the ProvisioningRequest CR",
			slog.String("name", t.object.Name),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.Validated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The provisioning request validation succeeded",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

// validateAndLoadTimeouts validates and loads timeout values from configmaps for
// hardware provisioning, cluster provisioning, and configuration into timeouts variable.
// If a timeout is not defined in the configmap, the default timeout value is used.
func (t *provisioningRequestReconcilerTask) validateAndLoadTimeouts(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {
	// Initialize with default timeouts
	t.timeouts.clusterProvisioningMinutes = utils.DefaultClusterProvisioningTimeoutMinutes
	t.timeouts.hardwareProvisioningMinutes = utils.DefaultHardwareProvisioningTimeoutMinutes
	t.timeouts.configurationMinutes = utils.DefaultClusterConfigurationTimeoutMinutes

	// Load hardware provisioning timeout if exists.
	hwCmName := clusterTemplate.Spec.Templates.HwTemplate
	hwCm, err := utils.GetConfigmap(
		ctx, t.client, hwCmName, utils.InventoryNamespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", hwCmName, err)
	}
	hwTimeout, err := utils.ExtractTimeoutFromConfigMap(
		hwCm, utils.HardwareProvisioningTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for hardware provisioning: %w", err)
	}
	if hwTimeout != 0 {
		t.timeouts.hardwareProvisioningMinutes = hwTimeout
	}

	// Load cluster provisioning timeout if exists.
	ciCmName := clusterTemplate.Spec.Templates.ClusterInstanceDefaults
	ciCm, err := utils.GetConfigmap(
		ctx, t.client, ciCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ciCmName, err)
	}
	ciTimeout, err := utils.ExtractTimeoutFromConfigMap(
		ciCm, utils.ClusterProvisioningTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for cluster provisioning: %w", err)
	}
	if ciTimeout != 0 {
		t.timeouts.clusterProvisioningMinutes = ciTimeout
	}

	// Load configuration timeout if exists.
	ptCmName := clusterTemplate.Spec.Templates.PolicyTemplateDefaults
	ptCm, err := utils.GetConfigmap(
		ctx, t.client, ptCmName, clusterTemplate.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get ConfigMap %s: %w", ptCmName, err)
	}
	ptTimeout, err := utils.ExtractTimeoutFromConfigMap(
		ptCm, utils.ClusterConfigurationTimeoutConfigKey)
	if err != nil {
		return fmt.Errorf("failed to get timeout config for cluster configuration: %w", err)
	}
	if ptTimeout != 0 {
		t.timeouts.configurationMinutes = ptTimeout
	}
	return nil
}

// validateProvisioningRequestCR validates the ProvisioningRequest CR
func (t *provisioningRequestReconcilerTask) validateProvisioningRequestCR(ctx context.Context) error {
	// Check the referenced cluster template is present and valid
	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}
	ctValidatedCondition := meta.FindStatusCondition(clusterTemplate.Status.Conditions, string(utils.CTconditionTypes.Validated))
	if ctValidatedCondition == nil || ctValidatedCondition.Status == metav1.ConditionFalse {
		return utils.NewInputError("the clustertemplate validation has failed")
	}

	if err = t.validateTemplateInputMatchesSchema(clusterTemplate); err != nil {
		return utils.NewInputError(err.Error())
	}

	if err = t.validateClusterInstanceInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate ClusterInstance input: %w", err)
	}

	if err = t.validatePolicyTemplateInputMatchesSchema(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to validate PolicyTemplate input: %w", err)
	}

	if err = t.validateAndLoadTimeouts(ctx, clusterTemplate); err != nil {
		return fmt.Errorf("failed to load timeouts: %w", err)
	}
	// TODO: Verify that ClusterInstance is per ClusterRequest basis.
	//       There should not be multiple ClusterRequests for the same ClusterInstance.
	return nil
}

// validateClusterInstanceInputMatchesSchema validates that the ClusterInstance input
// from the ProvisioningRequest matches the schema defined in the ClusterTemplate.
// If valid, the merged ClusterInstance data is stored in the clusterInput.
func (t *provisioningRequestReconcilerTask) validateClusterInstanceInputMatchesSchema(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	// Get the subschema for ClusterInstanceParameters
	clusterInstanceSubSchema, err := utils.ExtractSubSchema(
		clusterTemplate.Spec.TemplateParameterSchema.Raw, utils.TemplateParamClusterInstance)
	if err != nil {
		return utils.NewInputError(
			"failed to extract %s subschema: %s", utils.TemplateParamClusterInstance, err.Error())
	}
	// Any unknown fields not defined in the schema will be disallowed
	utils.DisallowUnknownFieldsInSchema(clusterInstanceSubSchema)

	// Get the matching input for ClusterInstanceParameters
	clusterInstanceMatchingInput, err := utils.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamClusterInstance)
	if err != nil {
		return utils.NewInputError(
			"failed to extract matching input for subSchema %s: %w", utils.TemplateParamClusterInstance, err)
	}
	clusterInstanceMatchingInputMap := clusterInstanceMatchingInput.(map[string]any)

	// The schema defined in ClusterTemplate's spec.templateParameterSchema for
	// clusterInstanceParameters represents a subschema of ClusterInstance parameters that
	// are allowed/exposed only to the ProvisioningRequest. Therefore, validate the ClusterInstance
	// input from the ProvisioningRequest against this schema, rather than validating the merged
	// ClusterInstance data. A full validation of the complete ClusterInstance input will be
	// performed during the ClusterInstance dry-run later.
	err = utils.ValidateJsonAgainstJsonSchema(
		clusterInstanceSubSchema, clusterInstanceMatchingInput)
	if err != nil {
		return utils.NewInputError(
			"the provided %s does not match the schema from ClusterTemplate (%s): %w",
			utils.TemplateParamClusterInstance, clusterTemplate.Name, err)
	}

	// Get the merged ClusterInstance input data
	mergedClusterInstanceData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.Templates.ClusterInstanceDefaults,
		clusterInstanceMatchingInputMap,
		utils.TemplateParamClusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	t.clusterInput.clusterInstanceData = mergedClusterInstanceData
	return nil
}

// validatePolicyTemplateInputMatchesSchema validates that the merged PolicyTemplate input
// (from both the ProvisioningRequest and the default configmap) matches the schema defined
// in the ClusterTemplate. If valid, the merged PolicyTemplate data is stored in clusterInput.
func (t *provisioningRequestReconcilerTask) validatePolicyTemplateInputMatchesSchema(
	ctx context.Context, clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {

	// Get the subschema for PolicyTemplateParameters
	policyTemplateSubSchema, err := utils.ExtractSubSchema(
		clusterTemplate.Spec.TemplateParameterSchema.Raw, utils.TemplateParamPolicyConfig)
	if err != nil {
		return utils.NewInputError(
			"failed to extract %s subschema: %s", utils.TemplateParamPolicyConfig, err.Error())
	}
	// Get the matching input for PolicyTemplateParameters
	policyTemplateMatchingInput, err := utils.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamPolicyConfig)
	if err != nil {
		return utils.NewInputError(
			"failed to extract matching input for subschema %s: %w", utils.TemplateParamPolicyConfig, err)
	}
	policyTemplateMatchingInputMap := policyTemplateMatchingInput.(map[string]any)

	// Get the merged PolicyTemplate input data
	mergedPolicyTemplateData, err := t.getMergedClusterInputData(
		ctx, clusterTemplate.Spec.Templates.PolicyTemplateDefaults,
		policyTemplateMatchingInputMap,
		utils.TemplateParamPolicyConfig)
	if err != nil {
		return fmt.Errorf("failed to get merged cluster input data: %w", err)
	}

	// Validate the merged PolicyTemplate input data matches the schema
	err = utils.ValidateJsonAgainstJsonSchema(
		policyTemplateSubSchema, mergedPolicyTemplateData)
	if err != nil {
		return utils.NewInputError(
			"the provided %s does not match the schema from ClusterTemplate (%s): %w",
			utils.TemplateParamPolicyConfig, clusterTemplate.Name, err)
	}

	t.clusterInput.policyTemplateData = mergedPolicyTemplateData
	return nil
}

// validateTemplateInputMatchesSchema validates the input parameters from the ProvisioningRequest
// against the schema defined in the ClusterTemplate. This function focuses on validating the
// input other than clusterInstanceParameters and policyTemplateParameters, as those will be
// validated separately. It ensures the input parameters have the expected types and any
// required parameters are present.
func (t *provisioningRequestReconcilerTask) validateTemplateInputMatchesSchema(
	clusterTemplate *provisioningv1alpha1.ClusterTemplate) error {
	// Unmarshal the full schema from the ClusterTemplate
	templateParamSchema := make(map[string]any)
	err := json.Unmarshal(clusterTemplate.Spec.TemplateParameterSchema.Raw, &templateParamSchema)
	if err != nil {
		// Unlikely to happen since it has been validated by API server
		return utils.NewInputError("error unmarshaling template schema: %w", err)
	}

	// Unmarshal the template input from the ProvisioningRequest
	templateParamsInput := make(map[string]any)
	if err = json.Unmarshal(t.object.Spec.TemplateParameters.Raw, &templateParamsInput); err != nil {
		// Unlikely to happen since it has been validated by API server
		return utils.NewInputError("error unmarshaling templateParameters: %w", err)
	}

	// The following errors of missing keys are unlikely since the schema should already
	// be validated by ClusterTemplate controller
	schemaProperties, ok := templateParamSchema["properties"]
	if !ok {
		return utils.NewInputError(
			"missing keyword 'properties' in the schema from ClusterTemplate (%s)", clusterTemplate.Name)
	}
	clusterInstanceSubSchema, ok := schemaProperties.(map[string]any)[utils.TemplateParamClusterInstance]
	if !ok {
		return utils.NewInputError(
			"missing required property '%s' in the schema from ClusterTemplate (%s)",
			utils.TemplateParamClusterInstance, clusterTemplate.Name)
	}
	policyTemplateSubSchema, ok := schemaProperties.(map[string]any)[utils.TemplateParamPolicyConfig]
	if !ok {
		return utils.NewInputError(
			"missing required property '%s' in the schema from ClusterTemplate (%s)",
			utils.TemplateParamPolicyConfig, clusterTemplate.Name)
	}

	// The ClusterInstance and PolicyTemplate parameters have their own specific validation rules
	// and will be handled separately. For now, remove the subschemas for those parameters to
	// ensure they are not validated at this stage.
	delete(clusterInstanceSubSchema.(map[string]any), "properties")
	delete(policyTemplateSubSchema.(map[string]any), "properties")

	err = utils.ValidateJsonAgainstJsonSchema(templateParamSchema, templateParamsInput)
	if err != nil {
		return utils.NewInputError(
			"the provided templateParameters does not match the schema from ClusterTemplate (%s): %w",
			clusterTemplate.Name, err)
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) getMergedClusterInputData(
	ctx context.Context, templateDefaultsCm string, clusterTemplateInput map[string]any, templateParam string) (map[string]any, error) {

	var templateDefaultsCmKey string

	switch templateParam {
	case utils.TemplateParamClusterInstance:
		templateDefaultsCmKey = utils.ClusterInstanceTemplateDefaultsConfigmapKey
	case utils.TemplateParamPolicyConfig:
		templateDefaultsCmKey = utils.PolicyTemplateDefaultsConfigmapKey
	default:
		return nil, utils.NewInputError("unsupported template parameter")
	}

	// Retrieve the configmap that holds the default data.
	templateCm, err := utils.GetConfigmap(ctx, t.client, templateDefaultsCm, t.ctNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", templateDefaultsCm, err)
	}
	clusterTemplateDefaultsMap, err := utils.ExtractTemplateDataFromConfigMap[map[string]any](
		templateCm, templateDefaultsCmKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get template defaults from ConfigMap %s: %w", templateDefaultsCm, err)
	}

	if templateParam == utils.TemplateParamClusterInstance {
		// Special handling for overrides of ClusterInstance's extraLabels and extraAnnotations.
		// The clusterTemplateInput will be overridden with the values from defaut configmap
		// if same labels/annotations exist in both.
		if err := utils.OverrideClusterInstanceLabelsOrAnnotations(
			clusterTemplateInput, clusterTemplateDefaultsMap); err != nil {
			return nil, utils.NewInputError(err.Error())
		}
	}

	// Get the merged cluster data
	mergedClusterDataMap, err := mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateDefaultsMap)
	if err != nil {
		return nil, utils.NewInputError("failed to merge data for %s: %s", templateParam, err.Error())
	}

	t.logger.Info(
		fmt.Sprintf("Merged the %s default data with the clusterTemplateInput data for ProvisioningRequest", templateParam),
		slog.String("name", t.object.Name),
	)
	return mergedClusterDataMap, nil
}

// handleRenderClusterInstance handles the ClusterInstance rendering and validation.
func (t *provisioningRequestReconcilerTask) handleRenderClusterInstance(ctx context.Context) (*siteconfig.ClusterInstance, error) {
	renderedClusterInstance, err := t.renderClusterInstanceTemplate(ctx)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render and validate the ClusterInstance for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render and validate ClusterInstance: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered the ClusterInstance and validated it with dry-run",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"ClusterInstance rendered and passed dry-run validation",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to handle ClusterInstance rendering and validation: %w", err)
	}
	return renderedClusterInstance, nil
}

func (t *provisioningRequestReconcilerTask) renderClusterInstanceTemplate(
	ctx context.Context) (*siteconfig.ClusterInstance, error) {
	t.logger.InfoContext(
		ctx,
		"Rendering the ClusterInstance template for ProvisioningRequest",
		slog.String("name", t.object.Name),
	)

	// Wrap the merged ClusterInstance data in a map with key "Cluster"
	// This data object will be consumed by the clusterInstance template
	mergedClusterInstanceData := map[string]any{
		"Cluster": t.clusterInput.clusterInstanceData,
	}

	renderedClusterInstance := &siteconfig.ClusterInstance{}
	renderedClusterInstanceUnstructure, err := utils.RenderTemplateForK8sCR(
		"ClusterInstance", utils.ClusterInstanceTemplatePath, mergedClusterInstanceData)
	if err != nil {
		return nil, utils.NewInputError("failed to render the ClusterInstance template for ProvisioningRequest: %w", err)
	} else {
		// Add ProvisioningRequest labels to the generated ClusterInstance
		labels := make(map[string]string)
		labels[provisioningRequestNameLabel] = t.object.Name
		renderedClusterInstanceUnstructure.SetLabels(labels)

		// Create the ClusterInstance namespace if not exist.
		ciName := renderedClusterInstanceUnstructure.GetName()
		err = t.createClusterInstanceNamespace(ctx, ciName)
		if err != nil {
			return nil, fmt.Errorf("failed to create cluster namespace %s: %w", ciName, err)
		}

		// Check for updates to immutable fields in the ClusterInstance, if it exists.
		// Once provisioning has started or reached a final state (Completed or Failed),
		// updates to immutable fields in the ClusterInstance spec are disallowed,
		// with the exception of scaling up/down when Cluster provisioning is completed.
		crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions,
			string(utils.PRconditionTypes.ClusterProvisioned))
		if crProvisionedCond != nil && crProvisionedCond.Reason != string(utils.CRconditionReasons.Unknown) {
			existingClusterInstance := &unstructured.Unstructured{}
			existingClusterInstance.SetGroupVersionKind(
				renderedClusterInstanceUnstructure.GroupVersionKind())
			ciExists, err := utils.DoesK8SResourceExist(
				ctx, t.client, ciName, ciName, existingClusterInstance,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get ClusterInstance (%s): %w",
					ciName, err)
			}
			if ciExists {
				updatedFields, scalingNodes, err := utils.FindClusterInstanceImmutableFieldUpdates(
					existingClusterInstance, renderedClusterInstanceUnstructure)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to find immutable field updates for ClusterInstance (%s): %w", ciName, err)
				}

				var disallowedChanges []string
				if len(updatedFields) != 0 {
					disallowedChanges = append(disallowedChanges, updatedFields...)
				}
				if len(scalingNodes) != 0 &&
					crProvisionedCond.Reason != string(utils.CRconditionReasons.Completed) {
					// In-progress || Failed
					disallowedChanges = append(disallowedChanges, scalingNodes...)
				}

				if len(disallowedChanges) != 0 {
					return nil, utils.NewInputError(fmt.Sprintf(
						"detected changes in immutable fields: %s", strings.Join(disallowedChanges, ", ")))
				}
			}
		}

		// Validate the rendered ClusterInstance with dry-run
		isDryRun := true
		err = t.applyClusterInstance(ctx, renderedClusterInstanceUnstructure, isDryRun)
		if err != nil {
			return nil, fmt.Errorf("failed to validate the rendered ClusterInstance with dry-run: %w", err)
		}

		// Convert unstructured to siteconfig.ClusterInstance type
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(
			renderedClusterInstanceUnstructure.Object, renderedClusterInstance); err != nil {
			// Unlikely to happen since dry-run validation has passed
			return nil, utils.NewInputError("failed to convert to siteconfig.ClusterInstance type: %w", err)
		}
	}

	return renderedClusterInstance, nil
}

func (t *provisioningRequestReconcilerTask) handleClusterResources(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	err := t.createOrUpdateClusterResources(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to apply the required cluster resource for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to apply the required cluster resource: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Applied the required cluster resources for ProvisioningRequest",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterResourcesCreated,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Cluster resources applied",
		)
	}
	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return err
}

func (t *provisioningRequestReconcilerTask) renderHardwareTemplate(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {
	renderedNodePool, err := t.handleRenderHardwareTemplate(ctx, clusterInstance)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to render the Hardware template for NodePool",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Failed,
			metav1.ConditionFalse,
			"Failed to render the Hardware template: "+err.Error(),
		)
	} else {
		t.logger.InfoContext(
			ctx,
			"Successfully rendered Hardware template for NodePool",
			slog.String("name", t.object.Name),
		)

		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.HardwareTemplateRendered,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"Rendered Hardware template successfully",
		)
	}

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return nil, fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return renderedNodePool, err
}

func (t *provisioningRequestReconcilerTask) handleRenderHardwareTemplate(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance) (*hwv1alpha1.NodePool, error) {

	nodePool := &hwv1alpha1.NodePool{}

	clusterTemplate, err := t.getCrClusterTemplateRef(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get the ClusterTemplate for ProvisioningRequest %s: %w ", t.object.Name, err)
	}

	hwTemplateCmName := clusterTemplate.Spec.Templates.HwTemplate
	hwTemplateCm, err := utils.GetConfigmap(ctx, t.client, hwTemplateCmName, utils.InventoryNamespace)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the %s configmap for Hardware Template, err: %w", hwTemplateCmName, err)
	}

	nodeGroup, err := utils.ExtractTemplateDataFromConfigMap[[]hwv1alpha1.NodeGroup](
		hwTemplateCm, utils.HwTemplateNodePool)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the Hardware template from ConfigMap %s, err: %w", hwTemplateCmName, err)
	}

	roleCounts := make(map[string]int)
	err = utils.ProcessClusterNodeGroups(clusterInstance, nodeGroup, roleCounts)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get the process node spec err: %w", err)
	}

	for i, group := range nodeGroup {
		if count, ok := roleCounts[group.Name]; ok {
			nodeGroup[i].Size = count
		}
	}

	siteId, err := utils.ExtractMatchingInput(
		t.object.Spec.TemplateParameters.Raw, utils.TemplateParamOCloudSiteId)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s from templateParameters: %w", utils.TemplateParamOCloudSiteId, err)
	}

	nodePool.Spec.CloudID = clusterInstance.GetName()
	nodePool.Spec.Site = siteId.(string)
	nodePool.Spec.HwMgrId = hwTemplateCm.Data[utils.HwTemplatePluginMgr]
	nodePool.Spec.NodeGroup = nodeGroup
	nodePool.ObjectMeta.Name = clusterInstance.GetName()
	nodePool.ObjectMeta.Namespace = utils.GetHwMgrPluginNS()

	// Add boot interface label to the generated nodePool
	annotation := make(map[string]string)
	annotation[utils.HwTemplateBootIfaceLabel] = hwTemplateCm.Data[utils.HwTemplateBootIfaceLabel]
	nodePool.SetAnnotations(annotation)

	// Add ProvisioningRequest labels to the generated nodePool
	labels := make(map[string]string)
	labels[provisioningRequestNameLabel] = t.object.Name
	nodePool.SetLabels(labels)
	return nodePool, nil
}

// mergeClusterTemplateInputWithDefaults merges the cluster template input with the default data
func mergeClusterTemplateInputWithDefaults(clusterTemplateInput, clusterTemplateInputDefaults map[string]any) (map[string]any, error) {
	// Initialize a map to hold the merged data
	var mergedClusterData map[string]any

	switch {
	case len(clusterTemplateInputDefaults) != 0 && len(clusterTemplateInput) != 0:
		// A shallow copy of src map
		// Both maps reference to the same underlying data
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)

		checkType := false
		err := utils.DeepMergeMaps(mergedClusterData, clusterTemplateInput, checkType) // clusterTemplateInput overrides the defaults
		if err != nil {
			return nil, fmt.Errorf("failed to merge the clusterTemplateInput(src) with the defaults(dst): %w", err)
		}
	case len(clusterTemplateInputDefaults) == 0 && len(clusterTemplateInput) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInput)
	case len(clusterTemplateInput) == 0 && len(clusterTemplateInputDefaults) != 0:
		mergedClusterData = maps.Clone(clusterTemplateInputDefaults)
	default:
		return nil, fmt.Errorf("expected clusterTemplateInput data not provided in either ProvisioningRequest or Configmap")
	}

	return mergedClusterData, nil
}

func (t *provisioningRequestReconcilerTask) applyClusterInstance(ctx context.Context, clusterInstance client.Object, isDryRun bool) error {
	var operationType string

	// Query the ClusterInstance and its status.
	existingClusterInstance := &siteconfig.ClusterInstance{}
	err := t.client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterInstance.GetName(),
			Namespace: clusterInstance.GetNamespace()},
		existingClusterInstance)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get ClusterInstance: %w", err)
		}

		operationType = utils.OperationTypeCreated
		opts := []client.CreateOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}

		// Create the ClusterInstance
		err = t.client.Create(ctx, clusterInstance, opts...)
		if err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to create ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError(err.Error())
		}
	} else {
		if _, ok := clusterInstance.(*siteconfig.ClusterInstance); ok {
			// No update needed, return
			if equality.Semantic.DeepEqual(existingClusterInstance.Spec,
				clusterInstance.(*siteconfig.ClusterInstance).Spec) {
				return nil
			}
		}

		// Make sure these fields from existing object are copied
		clusterInstance.SetResourceVersion(existingClusterInstance.GetResourceVersion())
		clusterInstance.SetFinalizers(existingClusterInstance.GetFinalizers())
		clusterInstance.SetLabels(existingClusterInstance.GetLabels())
		clusterInstance.SetAnnotations(existingClusterInstance.GetAnnotations())

		operationType = utils.OperationTypeUpdated
		opts := []client.PatchOption{}
		if isDryRun {
			opts = append(opts, client.DryRunAll)
			operationType = utils.OperationTypeDryRun
		}
		patch := client.MergeFrom(existingClusterInstance.DeepCopy())
		if err := t.client.Patch(ctx, clusterInstance, patch, opts...); err != nil {
			if !errors.IsInvalid(err) && !errors.IsBadRequest(err) {
				return fmt.Errorf("failed to patch ClusterInstance: %w", err)
			}
			// Invalid or webhook error
			return utils.NewInputError(err.Error())
		}
	}

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Rendered ClusterInstance %s in the namespace %s %s",
			clusterInstance.GetName(),
			clusterInstance.GetNamespace(),
			operationType,
		),
	)
	return nil
}

// checkClusterProvisionStatus checks the status of cluster provisioning
func (t *provisioningRequestReconcilerTask) checkClusterProvisionStatus(
	ctx context.Context, clusterInstanceName string) error {

	clusterInstance := &siteconfig.ClusterInstance{}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, clusterInstanceName, clusterInstanceName, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to get ClusterInstance %s: %w", clusterInstanceName, err)
	}
	if !exists {
		return nil
	}
	// Check ClusterInstance status and update the corresponding ProvisioningRequest status conditions.
	t.updateClusterInstanceProcessedStatus(clusterInstance)
	t.updateClusterProvisionStatus(clusterInstance)

	if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
		return fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
	}

	return nil
}

// handleClusterInstallation creates/updates the ClusterInstance to handle the cluster provisioning.
func (t *provisioningRequestReconcilerTask) handleClusterInstallation(ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	isDryRun := false
	err := t.applyClusterInstance(ctx, clusterInstance, isDryRun)
	if err != nil {
		if !utils.IsInputError(err) {
			return fmt.Errorf("failed to apply the rendered ClusterInstance (%s): %s", clusterInstance.Name, err.Error())
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.NotApplied,
			metav1.ConditionFalse,
			fmt.Sprintf(
				"Failed to apply the rendered ClusterInstance (%s): %s",
				clusterInstance.Name, err.Error()),
		)
	} else {
		// Set ClusterDetails
		if t.object.Status.ClusterDetails == nil {
			t.object.Status.ClusterDetails = &provisioningv1alpha1.ClusterDetails{}
		}
		t.object.Status.ClusterDetails.Name = clusterInstance.GetName()
	}

	// Continue checking the existing ClusterInstance provision status
	if err := t.checkClusterProvisionStatus(ctx, clusterInstance.Name); err != nil {
		return err
	}
	return nil
}

// handleClusterPolicyConfiguration updates the ProvisioningRequest status to reflect the status
// of the policies that match the managed cluster created through the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) handleClusterPolicyConfiguration(ctx context.Context) (
	requeue bool, err error) {
	if t.object.Status.ClusterDetails == nil {
		return false, fmt.Errorf("status.clusterDetails is empty")
	}

	// Get all the child policies in the namespace of the managed cluster created through
	// the ProvisioningRequest.
	policies := &policiesv1.PolicyList{}
	listOpts := []client.ListOption{
		client.HasLabels{utils.ChildPolicyRootPolicyLabel},
		client.InNamespace(t.object.Status.ClusterDetails.Name),
	}

	err = t.client.List(ctx, policies, listOpts...)
	if err != nil {
		return false, fmt.Errorf("failed to list Policies: %w", err)
	}

	allPoliciesCompliant := true
	nonCompliantPolicyInEnforce := false
	var targetPolicies []provisioningv1alpha1.PolicyDetails
	// Go through all the policies and get those that are matched with the managed cluster created
	// by the current provisioning request.
	for _, policy := range policies.Items {
		if policy.Status.ComplianceState != policiesv1.Compliant {
			allPoliciesCompliant = false
			if strings.EqualFold(string(policy.Spec.RemediationAction), string(policiesv1.Enforce)) {
				nonCompliantPolicyInEnforce = true
			}
		}
		// Child policy name = parent_policy_namespace.parent_policy_name
		policyNameArr := strings.Split(policy.Name, ".")
		targetPolicy := &provisioningv1alpha1.PolicyDetails{
			Compliant:         string(policy.Status.ComplianceState),
			PolicyName:        policyNameArr[1],
			PolicyNamespace:   policyNameArr[0],
			RemediationAction: string(policy.Spec.RemediationAction),
		}
		targetPolicies = append(targetPolicies, *targetPolicy)
	}
	err = t.updateConfigurationAppliedStatus(
		ctx, targetPolicies, allPoliciesCompliant, nonCompliantPolicyInEnforce)
	if err != nil {
		return false, err
	}
	err = t.updateZTPStatus(ctx, allPoliciesCompliant)
	if err != nil {
		return false, err
	}

	// If there are policies that are not Compliant, we need to requeue and see if they
	// time out or complete.
	return nonCompliantPolicyInEnforce, nil
}

// updateZTPStatus updates status.ClusterDetails.ZtpStatus.
func (t *provisioningRequestReconcilerTask) updateZTPStatus(ctx context.Context, allPoliciesCompliant bool) error {
	// Check if the cluster provision has started.
	crProvisionedCond := meta.FindStatusCondition(t.object.Status.Conditions, string(utils.PRconditionTypes.ClusterProvisioned))
	if crProvisionedCond != nil {
		// If the provisioning has started, and the ZTP status is empty or not done.
		if t.object.Status.ClusterDetails.ZtpStatus != utils.ClusterZtpDone {
			t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpNotDone
			// If the provisioning finished and all the policies are compliant, then ZTP is done.
			if crProvisionedCond.Status == metav1.ConditionTrue && allPoliciesCompliant {
				// Once the ZTPStatus reaches ZTP Done, it will stay that way.
				t.object.Status.ClusterDetails.ZtpStatus = utils.ClusterZtpDone
			}
		}
	}

	if err := utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		return fmt.Errorf("failed to update the ZTP status for ProvisioningRequest %s: %w", t.object.Name, err)
	}
	return nil
}

// hasPolicyConfigurationTimedOut determines if the policy configuration for the
// ProvisioningRequest has timed out.
func (t *provisioningRequestReconcilerTask) hasPolicyConfigurationTimedOut(ctx context.Context) bool {
	policyTimedOut := false
	// Get the ConfigurationApplied condition.
	configurationAppliedCondition := meta.FindStatusCondition(
		t.object.Status.Conditions,
		string(utils.PRconditionTypes.ConfigurationApplied))

	// If the condition does not exist, set the non compliant timestamp since we
	// get here just for policies that have a status different from Compliant.
	if configurationAppliedCondition == nil {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		return policyTimedOut
	}

	// If the current status of the Condition is false.
	if configurationAppliedCondition.Status == metav1.ConditionFalse {
		switch configurationAppliedCondition.Reason {
		case string(utils.CRconditionReasons.InProgress):
			// Check if the configuration application has timed out.
			if t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
			} else {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.configurationMinutes)
			}
		case string(utils.CRconditionReasons.TimedOut):
			policyTimedOut = true
		case string(utils.CRconditionReasons.Missing):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.OutOfDate):
			t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
		case string(utils.CRconditionReasons.ClusterNotReady):
			// The cluster might not be ready because its being initially provisioned or
			// there are problems after provisionion, so it might be that NonCompliantAt
			// has been previously set.
			if !t.object.Status.ClusterDetails.NonCompliantAt.IsZero() {
				// If NonCompliantAt has been previously set, check for timeout.
				policyTimedOut = utils.TimeoutExceeded(
					t.object.Status.ClusterDetails.NonCompliantAt.Time,
					t.timeouts.configurationMinutes)
			}
		default:
			t.logger.InfoContext(ctx,
				fmt.Sprintf("Unexpected Reason for condition type %s",
					utils.PRconditionTypes.ConfigurationApplied,
				),
			)
		}
	} else if configurationAppliedCondition.Reason == string(utils.CRconditionReasons.Completed) {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Now()
	}

	return policyTimedOut
}

// updateConfigurationAppliedStatus updates the ProvisioningRequest ConfigurationApplied condition
// based on the state of the policies matched with the managed cluster.
func (t *provisioningRequestReconcilerTask) updateConfigurationAppliedStatus(
	ctx context.Context, targetPolicies []provisioningv1alpha1.PolicyDetails, allPoliciesCompliant bool,
	nonCompliantPolicyInEnforce bool) (err error) {
	err = nil

	defer func() {
		t.object.Status.Policies = targetPolicies
		// Update the current policy status.
		if updateErr := utils.UpdateK8sCRStatus(ctx, t.client, t.object); updateErr != nil {
			err = fmt.Errorf("failed to update status for ProvisioningRequest %s: %w", t.object.Name, updateErr)
		} else {
			err = nil
		}
	}()

	if len(targetPolicies) == 0 {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Missing,
			metav1.ConditionFalse,
			"No configuration present",
		)
		return
	}

	// Update the ConfigurationApplied condition.
	if allPoliciesCompliant {
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.Completed,
			metav1.ConditionTrue,
			"The configuration is up to date",
		)
		return
	}

	clusterIsReadyForPolicyConfig, err := utils.ClusterIsReadyForPolicyConfig(
		ctx, t.client, t.object.Status.ClusterDetails.Name,
	)
	if err != nil {
		return fmt.Errorf(
			"error determining if the cluster is ready for policy configuration: %w", err)
	}

	if !clusterIsReadyForPolicyConfig {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"Cluster %s (%s) is not ready for policy configuration",
				t.object.Status.ClusterDetails.Name,
				t.object.Status.ClusterDetails.Name,
			),
		)
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.ClusterNotReady,
			metav1.ConditionFalse,
			"The Cluster is not yet ready",
		)
		return
	}

	if nonCompliantPolicyInEnforce {
		policyTimedOut := t.hasPolicyConfigurationTimedOut(ctx)

		message := "The configuration is still being applied"
		reason := utils.CRconditionReasons.InProgress
		if policyTimedOut {
			message += ", but it timed out"
			reason = utils.CRconditionReasons.TimedOut
		}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			reason,
			metav1.ConditionFalse,
			message,
		)
	} else {
		// No timeout is reported if all policies are in inform, just out of date.
		t.object.Status.ClusterDetails.NonCompliantAt = metav1.Time{}
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ConfigurationApplied,
			utils.CRconditionReasons.OutOfDate,
			metav1.ConditionFalse,
			"The configuration is out of date",
		)
	}

	return
}

func (t *provisioningRequestReconcilerTask) updateClusterInstanceProcessedStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	clusterInstanceConditionTypes := []string{
		"ClusterInstanceValidated",
		"RenderedTemplates",
		"RenderedTemplatesValidated",
		"RenderedTemplatesApplied",
	}

	if len(ci.Status.Conditions) == 0 {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterInstanceProcessed,
			utils.CRconditionReasons.Unknown,
			metav1.ConditionUnknown,
			fmt.Sprintf("Waiting for ClusterInstance (%s) to be processed", ci.Name),
		)
		return
	}

	for _, condType := range clusterInstanceConditionTypes {
		ciCondition := meta.FindStatusCondition(ci.Status.Conditions, condType)
		if ciCondition != nil && ciCondition.Status != metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.PRconditionTypes.ClusterInstanceProcessed,
				utils.ConditionReason(ciCondition.Reason),
				ciCondition.Status,
				ciCondition.Message,
			)

			return
		}
	}

	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.PRconditionTypes.ClusterInstanceProcessed,
		utils.CRconditionReasons.Completed,
		metav1.ConditionTrue,
		fmt.Sprintf("Applied and processed ClusterInstance (%s) successfully", ci.Name),
	)
}

func (t *provisioningRequestReconcilerTask) updateClusterProvisionStatus(ci *siteconfig.ClusterInstance) {
	if ci == nil {
		return
	}

	// Search for ClusterInstance Provisioned condition
	ciProvisionedCondition := meta.FindStatusCondition(
		ci.Status.Conditions, string(hwv1alpha1.Provisioned))

	if ciProvisionedCondition == nil {
		crClusterInstanceProcessedCond := meta.FindStatusCondition(
			t.object.Status.Conditions, string(utils.PRconditionTypes.ClusterInstanceProcessed))
		if crClusterInstanceProcessedCond != nil && crClusterInstanceProcessedCond.Status == metav1.ConditionTrue {
			utils.SetStatusCondition(&t.object.Status.Conditions,
				utils.PRconditionTypes.ClusterProvisioned,
				utils.CRconditionReasons.Unknown,
				metav1.ConditionUnknown,
				"Waiting for cluster provisioning to start",
			)
		}
	} else {
		utils.SetStatusCondition(&t.object.Status.Conditions,
			utils.PRconditionTypes.ClusterProvisioned,
			utils.ConditionReason(ciProvisionedCondition.Reason),
			ciProvisionedCondition.Status,
			ciProvisionedCondition.Message,
		)
	}

	if utils.IsClusterProvisionPresent(t.object) {
		// Set the start timestamp if it's not already set
		if t.object.Status.ClusterDetails.ClusterProvisionStartedAt.IsZero() {
			t.object.Status.ClusterDetails.ClusterProvisionStartedAt = metav1.Now()
		}

		// If it's not failed or completed, check if it has timed out
		if !utils.IsClusterProvisionCompletedOrFailed(t.object) {
			if utils.TimeoutExceeded(
				t.object.Status.ClusterDetails.ClusterProvisionStartedAt.Time,
				t.timeouts.clusterProvisioningMinutes) {
				// timed out
				utils.SetStatusCondition(&t.object.Status.Conditions,
					utils.PRconditionTypes.ClusterProvisioned,
					utils.CRconditionReasons.TimedOut,
					metav1.ConditionFalse,
					"Cluster provisioning timed out",
				)
			}
		}
	}
}

// createOrUpdateClusterResources creates/updates all the resources needed for cluster deployment
func (t *provisioningRequestReconcilerTask) createOrUpdateClusterResources(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterName := clusterInstance.GetName()

	// TODO: remove the BMC secrets creation when hw plugin is ready
	err := t.createClusterInstanceBMCSecrets(ctx, clusterName)
	if err != nil {
		return err
	}

	// Copy the pull secret from the cluster template namespace to the
	// clusterInstance namespace.
	err = t.createPullSecret(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create pull Secret for cluster %s: %w", clusterName, err)
	}

	// Copy the extra-manifests ConfigMaps from the cluster template namespace
	// to the clusterInstance namespace.
	err = t.createExtraManifestsConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create extraManifests ConfigMap for cluster %s: %w", clusterName, err)
	}

	// Create the cluster ConfigMap which will be used by ACM policies.
	err = t.createPoliciesConfigMap(ctx, clusterInstance)
	if err != nil {
		return fmt.Errorf("failed to create policy template ConfigMap for cluster %s: %w", clusterName, err)
	}

	return nil
}

// createExtraManifestsConfigMap copies the extra-manifests ConfigMaps from the
// cluster template namespace to the clusterInstance namespace.
func (t *provisioningRequestReconcilerTask) createExtraManifestsConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {
	for _, extraManifestsRef := range clusterInstance.Spec.ExtraManifestsRefs {
		// Make sure the extra-manifests ConfigMap exists in the clusterTemplate namespace.
		configMap := &corev1.ConfigMap{}
		extraManifestCmName := extraManifestsRef.Name
		configMapExists, err := utils.DoesK8SResourceExist(
			ctx, t.client, extraManifestCmName, t.ctNamespace, configMap)
		if err != nil {
			return fmt.Errorf("failed to check if ConfigMap exists: %w", err)
		}
		if !configMapExists {
			return utils.NewInputError(
				"extra-manifests configmap %s expected to exist in the %s namespace, but it is missing",
				extraManifestCmName, t.ctNamespace)
		}

		// Create the extra-manifests ConfigMap in the clusterInstance namespace
		newExtraManifestsConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      extraManifestCmName,
				Namespace: clusterInstance.Name,
			},
			Data: configMap.Data,
		}
		if err := utils.CreateK8sCR(ctx, t.client, newExtraManifestsConfigMap, t.object, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create extra-manifests ConfigMap: %w", err)
		}
	}

	return nil
}

// createPullSecret copies the pull secret from the cluster template namespace
// to the clusterInstance namespace
func (t *provisioningRequestReconcilerTask) createPullSecret(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	clusterTemplateRefName := getClusterTemplateRefName(
		t.object.Spec.TemplateName, t.object.Spec.TemplateVersion)
	// If we got to this point, we can assume that all the keys exist, including
	// clusterName

	// Check the pull secret already exists in the clusterTemplate namespace.
	pullSecret := &corev1.Secret{}
	pullSecretName := clusterInstance.Spec.PullSecretRef.Name
	pullSecretExistsInTemplateNamespace, err := utils.DoesK8SResourceExist(
		ctx, t.client, pullSecretName, t.ctNamespace, pullSecret)
	if err != nil {
		return fmt.Errorf(
			"failed to check if pull secret %s exists in namespace %s: %w",
			pullSecretName, clusterTemplateRefName, err,
		)
	}
	if !pullSecretExistsInTemplateNamespace {
		return utils.NewInputError(
			"pull secret %s expected to exist in the %s namespace, but it is missing",
			pullSecretName, clusterTemplateRefName)
	}

	newClusterInstancePullSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pullSecretName,
			Namespace: clusterInstance.Name,
		},
		Data: pullSecret.Data,
		Type: corev1.SecretTypeDockerConfigJson,
	}

	if err := utils.CreateK8sCR(ctx, t.client, newClusterInstancePullSecret, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for ClusterInstancePullSecret: %w", err)
	}

	return nil
}

// createClusterInstanceNamespace creates the namespace of the ClusterInstance
// where all the other resources needed for installation will exist.
func (t *provisioningRequestReconcilerTask) createClusterInstanceNamespace(
	ctx context.Context, clusterName string) error {

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}

	// Add ProvisioningRequest labels to the namespace
	labels := make(map[string]string)
	labels[provisioningRequestNameLabel] = t.object.Name
	namespace.SetLabels(labels)

	err := utils.CreateK8sCR(ctx, t.client, namespace, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create or update namespace %s: %w", clusterName, err)
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return utils.NewInputError("the namespace %s is terminating", clusterName)
	}

	return nil
}

// createClusterInstanceBMCSecrets creates all the BMC secrets needed by the nodes included
// in the ProvisioningRequest.
func (t *provisioningRequestReconcilerTask) createClusterInstanceBMCSecrets( // nolint: unused
	ctx context.Context, clusterName string) error {

	// The BMC credential details are for now obtained from the ProvisioningRequest.
	clusterTemplateInputParams := make(map[string]any)
	err := json.Unmarshal(t.object.Spec.TemplateParameters.Raw, &clusterTemplateInputParams)
	if err != nil {
		// Unlikely to happen since it has been validated by API server
		return fmt.Errorf("error unmarshaling templateParameters: %w", err)
	}

	// If we got to this point, we can assume that all the keys up to the BMC details
	// exists since ClusterInstance has nodes mandatory.
	nodesInterface, nodesExist := clusterTemplateInputParams[utils.TemplateParamClusterInstance].(map[string]any)["nodes"]
	if !nodesExist {
		// Unlikely to happen
		return utils.NewInputError(
			"\"spec.nodes\" expected to exist in the rendered ClusterInstance for ProvisioningRequest %s, but it is missing",
			t.object.Name,
		)
	}

	nodes := nodesInterface.([]interface{})
	// Go through all the nodes.
	for _, nodeInterface := range nodes {
		node := nodeInterface.(map[string]interface{})

		username, password, secretName, err :=
			utils.GetBMCDetailsForClusterInstance(node, t.object.Name)
		if err != nil {
			// If a hwmgr plugin is being used, BMC details will not be in the provisioning request
			t.logger.InfoContext(ctx, "BMC details not present in provisioning request", "name", t.object.Name)
			continue
		}

		// Create the node's BMC secret.
		bmcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: clusterName,
			},
			Data: map[string][]byte{
				"username": []byte(username),
				"password": []byte(password),
			},
		}

		if err = utils.CreateK8sCR(ctx, t.client, bmcSecret, nil, utils.UPDATE); err != nil {
			return fmt.Errorf("failed to create Kubernetes CR: %w", err)
		}
	}

	return nil
}

// copyHwMgrPluginBMCSecret copies the BMC secret from the plugin namespace to the cluster namespace
func (t *provisioningRequestReconcilerTask) copyHwMgrPluginBMCSecret(ctx context.Context, name, sourceNamespace, targetNamespace string) error {

	// if the secret already exists in the target namespace, do nothing
	secret := &corev1.Secret{}
	exists, err := utils.DoesK8SResourceExist(
		ctx, t.client, name, targetNamespace, secret)
	if err != nil {
		return fmt.Errorf("failed to check if secret exists in namespace %s: %w", targetNamespace, err)
	}
	if exists {
		t.logger.Info(
			"BMC secret already exists in the cluster namespace",
			slog.String("name", name),
			slog.String("name", targetNamespace),
		)
		return nil
	}

	if err := utils.CopyK8sSecret(ctx, t.client, name, sourceNamespace, targetNamespace); err != nil {
		return fmt.Errorf("failed to copy Kubernetes secret: %w", err)
	}

	return nil
}

// createHwMgrPluginNamespace creates the namespace of the hardware manager plugin
// where the node pools resource resides
func (t *provisioningRequestReconcilerTask) createHwMgrPluginNamespace(
	ctx context.Context, name string) error {

	t.logger.InfoContext(
		ctx,
		"Plugin: "+fmt.Sprintf("%v", name),
	)

	// Create the namespace.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := utils.CreateK8sCR(ctx, t.client, namespace, nil, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR for namespace %s: %w", namespace, err)
	}

	return nil
}

func (t *provisioningRequestReconcilerTask) hwMgrPluginNamespaceExists(
	ctx context.Context, name string) (bool, error) {

	t.logger.InfoContext(
		ctx,
		"Plugin: "+fmt.Sprintf("%v", name),
	)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, name, "", namespace)
	if err != nil {
		return false, fmt.Errorf("failed check if namespace exists %s: %w", name, err)
	}

	return exists, nil
}

func (t *provisioningRequestReconcilerTask) createNodePoolResources(ctx context.Context, nodePool *hwv1alpha1.NodePool) error {
	// Create the hardware plugin namespace.
	pluginNameSpace := nodePool.ObjectMeta.Namespace
	if exists, err := t.hwMgrPluginNamespaceExists(ctx, pluginNameSpace); err != nil {
		return fmt.Errorf("failed check if hardware manager plugin namespace exists %s, err: %w", pluginNameSpace, err)
	} else if !exists && (pluginNameSpace == utils.TempDellPluginNamespace || pluginNameSpace == utils.UnitTestHwmgrNamespace) {
		// TODO: For test purposes only. Code to be removed once hwmgr plugin(s) are fully utilized
		createErr := t.createHwMgrPluginNamespace(ctx, pluginNameSpace)
		if createErr != nil {
			return fmt.Errorf(
				"failed to create hardware manager plugin namespace %s, err: %w", pluginNameSpace, createErr)
		}
	} else if !exists {
		return fmt.Errorf("specified hardware manager plugin namespace does not exist: %s", pluginNameSpace)
	}

	// Create/update the clusterInstance namespace, adding ProvisioningRequest labels to the namespace
	err := t.createClusterInstanceNamespace(ctx, nodePool.GetName())
	if err != nil {
		return err
	}

	// Create the node pool resource
	createErr := utils.CreateK8sCR(ctx, t.client, nodePool, t.object, "")
	if createErr != nil {
		t.logger.ErrorContext(
			ctx,
			fmt.Sprintf(
				"Failed to create the NodePool %s in the namespace %s",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
			slog.String("error", createErr.Error()),
		)
		return fmt.Errorf("failed to create/update the NodePool: %s", createErr.Error())
	}

	// Set NodePoolRef
	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &provisioningv1alpha1.NodePoolRef{}
	}
	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()

	t.logger.InfoContext(
		ctx,
		fmt.Sprintf(
			"Created NodePool %s in the namespace %s, if not already exist",
			nodePool.GetName(),
			nodePool.GetNamespace(),
		),
	)
	// Set the CloudManager's ObservedGeneration on the node pool resource status field
	err = utils.SetCloudManagerGenerationStatus(ctx, t.client, nodePool)
	if err != nil {
		return fmt.Errorf("failed to set CloudManager's ObservedGeneration: %w", err)
	}
	return nil
}

func (t *provisioningRequestReconcilerTask) getCrClusterTemplateRef(ctx context.Context) (*provisioningv1alpha1.ClusterTemplate, error) {
	// Check the clusterTemplateRef references an existing template in the same namespace
	// as the current provisioningRequest.
	clusterTemplateRefName := getClusterTemplateRefName(
		t.object.Spec.TemplateName, t.object.Spec.TemplateVersion)
	clusterTemplates := &provisioningv1alpha1.ClusterTemplateList{}

	// Get the one clusterTemplate that's valid.
	err := t.client.List(ctx, clusterTemplates)
	// If there was an error in trying to get the ClusterTemplate, return it.
	if err != nil {
		return nil, fmt.Errorf("failed to get ClusterTemplate: %w", err)
	}
	for _, ct := range clusterTemplates.Items {
		if ct.Name == clusterTemplateRefName {
			validatedCond := meta.FindStatusCondition(
				ct.Status.Conditions,
				string(utils.CTconditionTypes.Validated))
			if validatedCond != nil && validatedCond.Status == metav1.ConditionTrue {
				t.ctNamespace = ct.Namespace
				return &ct, nil
			}
		}
	}

	// If the referenced ClusterTemplate does not exist, log and return an appropriate error.
	return nil, utils.NewInputError(
		fmt.Sprintf(
			"a valid (%s) ClusterTemplate does not exist in any namespace",
			clusterTemplateRefName))
}

// createPoliciesConfigMap creates the cluster ConfigMap which will be used
// by the ACM policies.
func (t *provisioningRequestReconcilerTask) createPoliciesConfigMap(
	ctx context.Context, clusterInstance *siteconfig.ClusterInstance) error {

	// Check the cluster version for the cluster-version label.
	clusterLabels := clusterInstance.Spec.ExtraLabels["ManagedCluster"]
	if err := utils.CheckClusterLabelsForPolicies(clusterInstance.Name, clusterLabels); err != nil {
		return fmt.Errorf("failed to check cluster labels: %w", err)
	}

	return t.createPolicyTemplateConfigMap(ctx, clusterInstance.Name)
}

// createPolicyTemplateConfigMap updates the keys of the default ConfigMap to match the
// clusterTemplate and the cluster version and creates/updates the ConfigMap for the
// required version of the policy template.
func (t *provisioningRequestReconcilerTask) createPolicyTemplateConfigMap(
	ctx context.Context, clusterName string) error {

	// If there is no policy configuration data, log a message and return without an error.
	if len(t.clusterInput.policyTemplateData) == 0 {
		t.logger.InfoContext(ctx, "Policy template data is empty")
		return nil
	}

	// Update the keys to match the ClusterTemplate name and the version.
	finalPolicyTemplateData := make(map[string]string)
	for key, value := range t.clusterInput.policyTemplateData {
		finalPolicyTemplateData[key] = value.(string)
	}

	// Put all the data from the mergedPolicyTemplateData in a configMap in the same
	// namespace as the templated ACM policies.
	// The namespace is: ztp + <clustertemplate-namespace>
	policyTemplateConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-pg", clusterName),
			Namespace: fmt.Sprintf("ztp-%s", t.ctNamespace),
		},
		Data: finalPolicyTemplateData,
	}

	if err := utils.CreateK8sCR(ctx, t.client, policyTemplateConfigMap, t.object, utils.UPDATE); err != nil {
		return fmt.Errorf("failed to create Kubernetes CR: %w", err)
	}

	return nil
}

func (r *ProvisioningRequestReconciler) finalizeProvisioningRequest(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) error {

	var labels = map[string]string{
		provisioningRequestNameLabel: provisioningRequest.Name,
	}
	listOpts := []client.ListOption{
		client.MatchingLabels(labels),
	}

	// Query the NodePool created by this ProvisioningRequest. Delete it if exists.
	nodePoolList := &hwv1alpha1.NodePoolList{}
	if err := r.Client.List(ctx, nodePoolList, listOpts...); err != nil {
		return fmt.Errorf("failed to list node pools: %w", err)
	}
	for _, nodePool := range nodePoolList.Items {
		copiedNodePool := nodePool
		if err := r.Client.Delete(ctx, &copiedNodePool); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete node pool: %w", err)
		}
	}

	// If the ClusterInstance has been created by this ProvisioningRequest, delete it.
	// The SiteConfig operator will also delete the namespace.
	clusterInstanceList := &siteconfig.ClusterInstanceList{}
	if err := r.Client.List(ctx, clusterInstanceList, listOpts...); err != nil {
		return fmt.Errorf("failed to list cluster instances: %w", err)
	}
	for _, clusterInstance := range clusterInstanceList.Items {
		copiedClusterInstance := clusterInstance
		if err := r.Client.Delete(ctx, &copiedClusterInstance); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete cluster instance: %w", err)
		}
	}

	if len(clusterInstanceList.Items) == 0 {
		// If the ClusterInstance has not been created. Query the namespace created by
		// this ProvisioningRequest. Delete it if exists.
		namespaceList := &corev1.NamespaceList{}
		if err := r.Client.List(ctx, namespaceList, listOpts...); err != nil {
			return fmt.Errorf("failed to list namespaces: %w", err)
		}
		for _, ns := range namespaceList.Items {
			copiedNamespace := ns
			if err := r.Client.Delete(ctx, &copiedNamespace); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("failed to delete namespace: %w", err)
			}
		}
	}

	return nil
}

func (r *ProvisioningRequestReconciler) handleFinalizer(
	ctx context.Context, provisioningRequest *provisioningv1alpha1.ProvisioningRequest) (ctrl.Result, bool, error) {

	// Check if the ProvisioningRequest is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	if provisioningRequest.DeletionTimestamp.IsZero() {
		// Check and add finalizer for this CR.
		if !controllerutil.ContainsFinalizer(provisioningRequest, provisioningRequestFinalizer) {
			controllerutil.AddFinalizer(provisioningRequest, provisioningRequestFinalizer)
			// Update and requeue since the finalizer has been added.
			if err := r.Update(ctx, provisioningRequest); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to update ProvisioningRequest with finalizer: %w", err)
			}
			return ctrl.Result{Requeue: true}, true, nil
		}
		return ctrl.Result{}, false, nil
	} else if controllerutil.ContainsFinalizer(provisioningRequest, provisioningRequestFinalizer) {
		// Run finalization logic for provisioningRequestFinalizer. If the finalization logic
		// fails, don't remove the finalizer so that we can retry during the next reconciliation.
		if err := r.finalizeProvisioningRequest(ctx, provisioningRequest); err != nil {
			return ctrl.Result{}, true, err
		}

		// Remove provisioningRequestFinalizer. Once all finalizers have been
		// removed, the object will be deleted.
		r.Logger.Info("Removing provisioningRequest finalizer", "name", provisioningRequest.Name)
		patch := client.MergeFrom(provisioningRequest.DeepCopy())
		if controllerutil.RemoveFinalizer(provisioningRequest, provisioningRequestFinalizer) {
			if err := r.Patch(ctx, provisioningRequest, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("failed to patch ProvisioningRequest: %w", err)
			}
			return ctrl.Result{}, true, nil
		}
	}
	return ctrl.Result{}, false, nil
}

// checkNodePoolProvisionStatus checks for the NodePool status to be in the provisioned state.
func (t *provisioningRequestReconcilerTask) checkNodePoolProvisionStatus(ctx context.Context,
	nodePool *hwv1alpha1.NodePool) (bool, bool, error) {

	// Get the generated NodePool and its status.
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodePool.GetName(),
		nodePool.GetNamespace(), nodePool)

	if err != nil {
		return false, false, fmt.Errorf("failed to get node pool; %w", err)
	}
	if !exists {
		return false, false, fmt.Errorf("node pool does not exist")
	}

	// Update the provisioning request Status with status from the NodePool object.
	provisioned, timedOutOrFailed, err := t.updateHardwareProvisioningStatus(ctx, nodePool)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update the NodePool status for ProvisioningRequest",
			slog.String("name", t.object.Name),
			slog.String("error", err.Error()),
		)
	}

	return provisioned, timedOutOrFailed, err
}

// updateClusterInstance updates the given ClusterInstance object based on the provisioned nodePool.
func (t *provisioningRequestReconcilerTask) updateClusterInstance(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) bool {

	hwNodes := t.collectNodeDetails(ctx, nodePool)
	if hwNodes == nil {
		return false
	}

	if !t.copyBMCSecrets(ctx, hwNodes, nodePool) {
		return false
	}

	if !t.applyNodeConfiguration(ctx, hwNodes, nodePool, clusterInstance) {
		return false
	}

	return true
}

// waitForHardwareData waits for the NodePool to be provisioned and update BMC details
// and bootMacAddress in ClusterInstance.
func (t *provisioningRequestReconcilerTask) waitForHardwareData(ctx context.Context,
	clusterInstance *siteconfig.ClusterInstance, nodePool *hwv1alpha1.NodePool) (bool, bool, error) {

	provisioned, timedOutOrFailed, err := t.checkNodePoolProvisionStatus(ctx, nodePool)
	if provisioned && err == nil {
		t.logger.InfoContext(
			ctx,
			fmt.Sprintf(
				"NodePool %s in the namespace %s is provisioned",
				nodePool.GetName(),
				nodePool.GetNamespace(),
			),
		)
		if !t.updateClusterInstance(ctx, clusterInstance, nodePool) {
			err = fmt.Errorf("failed to update the rendered cluster instance")
		}
	}
	return provisioned, timedOutOrFailed, err
}

// collectNodeDetails collects BMC and node interfaces details
func (t *provisioningRequestReconcilerTask) collectNodeDetails(ctx context.Context,
	nodePool *hwv1alpha1.NodePool) map[string][]nodeInfo {

	// hwNodes maps a group name to a slice of NodeInfo
	hwNodes := make(map[string][]nodeInfo)

	for _, nodeName := range nodePool.Status.Properties.NodeNames {
		node := &hwv1alpha1.Node{}
		exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodeName, nodePool.Namespace, node)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to get the Node object",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.String("error", err.Error()),
				slog.Bool("exists", exists),
			)
			return nil
		}
		if !exists {
			t.logger.ErrorContext(
				ctx,
				"Node object does not exist",
				slog.String("name", nodeName),
				slog.String("namespace", nodePool.Namespace),
				slog.Bool("exists", exists),
			)
			return nil
		}
		// Verify the node object is generated from the expected pool
		if node.Spec.NodePool != nodePool.GetName() {
			t.logger.ErrorContext(
				ctx,
				"Node object is not from the expected NodePool",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil
		}

		if node.Status.BMC == nil {
			t.logger.ErrorContext(
				ctx,
				"Node status does not have BMC details",
				slog.String("name", node.GetName()),
				slog.String("pool", nodePool.GetName()),
			)
			return nil
		}
		// Store the nodeInfo per group
		hwNodes[node.Spec.GroupName] = append(hwNodes[node.Spec.GroupName], nodeInfo{
			bmcAddress:     node.Status.BMC.Address,
			bmcCredentials: node.Status.BMC.CredentialsName,
			nodeName:       node.Name,
			interfaces:     node.Status.Interfaces,
		})
	}

	return hwNodes
}

// copyBMCSecrets copies BMC secrets from the plugin namespace to the cluster namespace.
func (t *provisioningRequestReconcilerTask) copyBMCSecrets(ctx context.Context, hwNodes map[string][]nodeInfo,
	nodePool *hwv1alpha1.NodePool) bool {

	for _, nodeInfos := range hwNodes {
		for _, node := range nodeInfos {
			err := t.copyHwMgrPluginBMCSecret(ctx, node.bmcCredentials, nodePool.GetNamespace(), nodePool.GetName())
			if err != nil {
				t.logger.ErrorContext(
					ctx,
					"Failed to copy BMC secret from the plugin namespace to the cluster namespace",
					slog.String("name", node.bmcCredentials),
					slog.String("plugin", nodePool.GetNamespace()),
					slog.String("cluster", nodePool.GetName()),
				)
				return false
			}
		}
	}
	return true
}

// applyNodeConfiguration updates the clusterInstance with BMC details, interface MACAddress and bootMACAddress
func (t *provisioningRequestReconcilerTask) applyNodeConfiguration(ctx context.Context, hwNodes map[string][]nodeInfo,
	nodePool *hwv1alpha1.NodePool, clusterInstance *siteconfig.ClusterInstance) bool {

	for i, node := range clusterInstance.Spec.Nodes {
		// Check if the node's role matches any key in hwNodes
		nodeInfos, exists := hwNodes[node.Role]
		if !exists || len(nodeInfos) == 0 {
			continue
		}

		clusterInstance.Spec.Nodes[i].BmcAddress = nodeInfos[0].bmcAddress
		clusterInstance.Spec.Nodes[i].BmcCredentialsName = siteconfig.BmcCredentialsName{Name: nodeInfos[0].bmcCredentials}
		// Get the boot MAC address based on the interface label
		bootMAC, err := utils.GetBootMacAddress(nodeInfos[0].interfaces, nodePool)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Fail to get the node boot MAC address",
				slog.String("name", node.HostName),
				slog.String("error", err.Error()),
			)
			return false
		}
		clusterInstance.Spec.Nodes[i].BootMACAddress = bootMAC

		// Populate the MAC address for each interface
		for j, iface := range clusterInstance.Spec.Nodes[i].NodeNetwork.Interfaces {
			for _, nodeIface := range nodeInfos[0].interfaces {
				if nodeIface.Name == iface.Name {
					clusterInstance.Spec.Nodes[i].NodeNetwork.Interfaces[j].MacAddress = nodeIface.MACAddress
				}
			}
		}

		// Indicates which host has been assigned to the node
		if !t.updateNodeStatusWithHostname(ctx, nodeInfos[0].nodeName, node.HostName,
			nodePool.Namespace) {
			return false
		}
		hwNodes[node.Role] = nodeInfos[1:]
	}
	return true
}

// Updates the Node status with the hostname after BMC information has been assigned.
func (t *provisioningRequestReconcilerTask) updateNodeStatusWithHostname(ctx context.Context, nodeName, hostname, namespace string) bool {
	node := &hwv1alpha1.Node{}
	exists, err := utils.DoesK8SResourceExist(ctx, t.client, nodeName, namespace, node)
	if err != nil || !exists {
		t.logger.ErrorContext(
			ctx,
			"Failed to get the Node object for updating hostname",
			slog.String("name", nodeName),
			slog.String("namespace", namespace),
			slog.String("error", err.Error()),
			slog.Bool("exists", exists),
		)
		return false
	}

	node.Status.Hostname = hostname
	err = t.client.Status().Update(ctx, node)
	if err != nil {
		t.logger.ErrorContext(
			ctx,
			"Failed to update Node status with hostname",
			slog.String("name", node.GetName()),
			slog.String("hostname", hostname),
			slog.String("namespace", namespace),
			slog.String("error", err.Error()),
		)
		return false
	}
	return true
}

// updateHardwareProvisioningStatus updates the status for the ProvisioningRequest
func (t *provisioningRequestReconcilerTask) updateHardwareProvisioningStatus(
	ctx context.Context, nodePool *hwv1alpha1.NodePool) (bool, bool, error) {
	var status metav1.ConditionStatus
	var reason string
	var message string
	var err error
	timedOutOrFailed := false // Default to false unless explicitly needed

	if t.object.Status.NodePoolRef == nil {
		t.object.Status.NodePoolRef = &provisioningv1alpha1.NodePoolRef{}
	}

	t.object.Status.NodePoolRef.Name = nodePool.GetName()
	t.object.Status.NodePoolRef.Namespace = nodePool.GetNamespace()
	if t.object.Status.NodePoolRef.HardwareProvisioningCheckStart.IsZero() {
		t.object.Status.NodePoolRef.HardwareProvisioningCheckStart = metav1.Now()
	}

	provisionedCondition := meta.FindStatusCondition(
		nodePool.Status.Conditions, string(hwv1alpha1.Provisioned))
	if provisionedCondition != nil {
		status = provisionedCondition.Status
		reason = provisionedCondition.Reason
		message = provisionedCondition.Message

		if provisionedCondition.Status == metav1.ConditionFalse && reason == string(hwv1alpha1.Failed) {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"NodePool %s in the namespace %s provisioning failed",
					nodePool.GetName(),
					nodePool.GetNamespace(),
				),
			)
			// Ensure a consistent message for the provisioning request, regardless of which plugin is used.
			message = "Hardware provisioning failed"
			timedOutOrFailed = true
		}
	} else {
		// No provisioning condition found, set the status to unknown.
		status = metav1.ConditionUnknown
		reason = string(utils.CRconditionReasons.Unknown)
		message = "Unknown state of hardware provisioning"
	}

	// Check for timeout if not already failed or provisioned
	if status != metav1.ConditionTrue && reason != string(hwv1alpha1.Failed) {
		if utils.TimeoutExceeded(
			t.object.Status.NodePoolRef.HardwareProvisioningCheckStart.Time,
			t.timeouts.hardwareProvisioningMinutes) {
			t.logger.InfoContext(
				ctx,
				fmt.Sprintf(
					"NodePool %s in the namespace %s provisioning timed out",
					nodePool.GetName(),
					nodePool.GetNamespace(),
				),
			)
			reason = string(hwv1alpha1.TimedOut)
			message = "Hardware provisioning timed out"
			status = metav1.ConditionFalse
			timedOutOrFailed = true
		}
	}

	// Set the status condition for hardware provisioning.
	utils.SetStatusCondition(&t.object.Status.Conditions,
		utils.PRconditionTypes.HardwareProvisioned,
		utils.ConditionReason(reason),
		status,
		message)

	// Update the CR status for the ProvisioningRequest.
	if err = utils.UpdateK8sCRStatus(ctx, t.client, t.object); err != nil {
		err = fmt.Errorf("failed to update HardwareProvisioning status: %w", err)
	}
	return status == metav1.ConditionTrue, timedOutOrFailed, err
}

// findClusterInstanceForProvisioningRequest maps the ClusterInstance created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) findClusterInstanceForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newClusterInstance := event.ObjectNew.(*siteconfig.ClusterInstance)
	crName, nameExists := newClusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current ClusterInstance.
		r.Logger.Info(
			"[findClusterInstanceForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findNodePoolForProvisioningRequest maps the NodePool created by a
// ProvisioningRequest to a reconciliation request.
func (r *ProvisioningRequestReconciler) findNodePoolForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	newNodePool := event.ObjectNew.(*hwv1alpha1.NodePool)

	crName, nameExists := newNodePool.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		// Create reconciling requests only for the ProvisioningRequest that has generated
		// the current NodePool.
		r.Logger.Info(
			"[findNodePoolForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findClusterTemplateForProvisioningRequest maps the ClusterTemplates used by ProvisioningRequests
// to reconciling requests for those ProvisioningRequests.
func (r *ProvisioningRequestReconciler) findClusterTemplateForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case, we can use either new or old object.
	newClusterTemplate := event.ObjectNew.(*provisioningv1alpha1.ClusterTemplate)

	// Get all the provisioningRequests.
	provisioningRequests := &provisioningv1alpha1.ProvisioningRequestList{}
	err := r.Client.List(ctx, provisioningRequests, client.InNamespace(newClusterTemplate.GetNamespace()))
	if err != nil {
		r.Logger.Error("[findProvisioningRequestsForClusterTemplate] Error listing ProvisioningRequests. ", "Error: ", err)
		return
	}

	// Create reconciling requests only for the ProvisioningRequests that are using the
	// current clusterTemplate.
	for _, provisioningRequest := range provisioningRequests.Items {
		clusterTemplateRefName := getClusterTemplateRefName(
			provisioningRequest.Spec.TemplateName, provisioningRequest.Spec.TemplateVersion)
		if clusterTemplateRefName == newClusterTemplate.GetName() {
			r.Logger.Info(
				"[findProvisioningRequestsForClusterTemplate] Add new reconcile request for ProvisioningRequest",
				"name", provisioningRequest.Name)
			queue.Add(
				reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name: provisioningRequest.Name,
					},
				},
			)
		}
	}
}

// findManagedClusterForProvisioningRequest maps the ManagedClusters created
// by ClusterInstances through ProvisioningRequests.
func (r *ProvisioningRequestReconciler) findManagedClusterForProvisioningRequest(
	ctx context.Context, event event.UpdateEvent,
	queue workqueue.RateLimitingInterface) {

	// For this case, we can use either new or old object.
	newManagedCluster := event.ObjectNew.(*clusterv1.ManagedCluster)

	// Get the ClusterInstance
	clusterInstance := &siteconfig.ClusterInstance{}
	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newManagedCluster.Name,
		Name:      newManagedCluster.Name,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as this ManagedCluster is not deployed/managed by ClusterInstance
			return
		}
		r.Logger.Error("[findManagedClusterForProvisioningRequest] Error getting ClusterInstance. ", "Error: ", err)
		return
	}

	crName, nameExists := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if nameExists {
		r.Logger.Info(
			"[findManagedClusterForProvisioningRequest] Add new reconcile request for ProvisioningRequest",
			"name", crName)
		queue.Add(
			reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: crName,
				},
			},
		)
	}
}

// findPoliciesForProvisioningRequests creates reconciliation requests for the ProvisioningRequests
// whose associated ManagedClusters have matched policies Updated or Deleted.
func findPoliciesForProvisioningRequests[T deleteOrUpdateEvent](
	ctx context.Context, c client.Client, e T, q workqueue.RateLimitingInterface) {

	policy := &policiesv1.Policy{}
	switch evt := any(e).(type) {
	case event.UpdateEvent:
		policy = evt.ObjectOld.(*policiesv1.Policy)
	case event.DeleteEvent:
		policy = evt.Object.(*policiesv1.Policy)
	default:
		// Only Update and Delete events are supported
		return
	}

	// Get the ClusterInstance.
	clusterInstance := &siteconfig.ClusterInstance{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: policy.Namespace,
		Name:      policy.Namespace,
	}, clusterInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Return as the ManagedCluster for this Namespace is not deployed/managed by ClusterInstance.
			return
		}
		return
	}

	ProvisioningRequest, okCR := clusterInstance.GetLabels()[provisioningRequestNameLabel]
	if okCR {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Name: ProvisioningRequest}})
	}
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ProvisioningRequestReconciler) handlePolicyEventDelete(
	ctx context.Context, e event.DeleteEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function for determining the corresponding ProvisioningRequest.
	findPoliciesForProvisioningRequests(ctx, r.Client, e, q)
}

// handlePolicyEvent handled Updates and Deleted events.
func (r *ProvisioningRequestReconciler) handlePolicyEventUpdate(
	ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {

	// Call the generic function.
	findPoliciesForProvisioningRequests(ctx, r.Client, e, q)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProvisioningRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	//nolint:wrapcheck
	return ctrl.NewControllerManagedBy(mgr).
		Named("o2ims-cluster-request").
		For(
			&provisioningv1alpha1.ProvisioningRequest{},
			// Watch for create and update event for ProvisioningRequest.
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&provisioningv1alpha1.ClusterTemplate{},
			handler.Funcs{UpdateFunc: r.findClusterTemplateForProvisioningRequest},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes only.
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&siteconfig.ClusterInstance{},
			handler.Funcs{
				UpdateFunc: r.findClusterInstanceForProvisioningRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on ClusterInstance status conditions changes only
					ciOld := e.ObjectOld.(*siteconfig.ClusterInstance)
					ciNew := e.ObjectNew.(*siteconfig.ClusterInstance)

					if ciOld.GetGeneration() == ciNew.GetGeneration() {
						if !equality.Semantic.DeepEqual(ciOld.Status.Conditions, ciNew.Status.Conditions) {
							return true
						}
					}
					return false
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&hwv1alpha1.NodePool{},
			handler.Funcs{
				UpdateFunc: r.findNodePoolForProvisioningRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Watch on status changes.
					// TODO: Filter on further conditions that the ProvisioningRequest is interested in
					return e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return true },
			})).
		Watches(
			&policiesv1.Policy{},
			handler.Funcs{
				UpdateFunc: r.handlePolicyEventUpdate,
				DeleteFunc: r.handlePolicyEventDelete,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := e.ObjectNew.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}

					policyNew := e.ObjectNew.(*policiesv1.Policy)
					policyOld := e.ObjectOld.(*policiesv1.Policy)

					// Process status.status and remediation action changes.
					return policyOld.Status.ComplianceState != policyNew.Status.ComplianceState ||
						(policyOld.Spec.RemediationAction != policyNew.Spec.RemediationAction)
				},
				CreateFunc:  func(e event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc: func(de event.DeleteEvent) bool {
					// Filter out updates to parent policies.
					if _, ok := de.Object.GetLabels()[utils.ChildPolicyRootPolicyLabel]; !ok {
						return false
					}
					return true
				},
			})).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.Funcs{
				UpdateFunc: r.findManagedClusterForProvisioningRequest,
			},
			builder.WithPredicates(predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Check if the event was adding the label "ztp-done".
					// Return true for that event only, and false for everything else.
					_, doneLabelExistsInOld := e.ObjectOld.GetLabels()[ztpDoneLabel]
					_, doneLabelExistsInNew := e.ObjectNew.GetLabels()[ztpDoneLabel]

					doneLabelAdded := !doneLabelExistsInOld && doneLabelExistsInNew

					var availableInNew, availableInOld bool
					availableCondition := meta.FindStatusCondition(
						e.ObjectOld.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInOld = true
					}
					availableCondition = meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionAvailable)
					if availableCondition != nil && availableCondition.Status == metav1.ConditionTrue {
						availableInNew = true
					}

					var hubAccepted bool
					acceptedCondition := meta.FindStatusCondition(
						e.ObjectNew.(*clusterv1.ManagedCluster).Status.Conditions,
						clusterv1.ManagedClusterConditionHubAccepted)
					if acceptedCondition != nil && acceptedCondition.Status == metav1.ConditionTrue {
						hubAccepted = true
					}

					return (doneLabelAdded && availableInNew && hubAccepted) ||
						(!availableInOld && availableInNew && doneLabelExistsInNew && hubAccepted)
				},
				CreateFunc:  func(ce event.CreateEvent) bool { return false },
				GenericFunc: func(ge event.GenericEvent) bool { return false },
				DeleteFunc:  func(de event.DeleteEvent) bool { return false },
			})).
		Complete(r)
}
