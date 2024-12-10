package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openshift-kni/oran-o2ims/internal/model"
	"github.com/openshift-kni/oran-o2ims/internal/service"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/utils"
	"github.com/openshift-kni/oran-o2ims/internal/service/resources/db/models"
)

// GenericModelConverter converts a db model object into an API object and returns it as an `any`
// so that it can be used generically since the API model doesn't implement a base interface.
type GenericModelConverter func(object interface{}) any

// persistObject persists an object to its database table.  If the object does not already have a
// persisted representation then it is created; otherwise any modified fields are updated in the
// database tuple.  The function returns both the before and after versions of the object.
func persistObject[T db.Model](ctx context.Context, db *pgxpool.Pool,
	object T, uuid uuid.UUID) (*T, *T, error) {
	var before, after *T
	// Store the object into the database handling cases for both insert/update separately so that we have access to the
	// before & after view of the data.
	var record, err = utils.Find[T](ctx, db, uuid)
	if errors.Is(err, utils.ErrNotFound) {
		// New object instance
		after, err = utils.Create[T](ctx, db, object)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create object '%s/%s': %w", object.TableName(), uuid, err)
		}

		slog.Debug("object inserted", "table", object.TableName(), "uuid", uuid, "record", after)
		return nil, after, nil
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to get object '%s/%s': %w", object.TableName(), uuid, err)
	}

	// Updated object instance
	before = record

	// We only need to update the fields that have actually changed so compare them to get the list of fields.
	tags := utils.CompareObjects(*record, object, "CreatedAt")
	if len(tags) == 0 {
		// This shouldn't happen since the generation id always changes
		after = before
		slog.Warn("no change detected on persisted object", "table", object.TableName(), "uuid", uuid)
		return before, after, nil
	}

	after, err = utils.Update[T](ctx, db, uuid, object, tags.Fields()...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update object '%s/%s': %w", object.TableName(), uuid, err)
	}

	slog.Debug("object updated",
		"table", object.TableName(), "uuid", uuid, "before", before, "after", after, "columns", tags.Fields())

	return before, after, nil
}

// persistDataChangeEvent persists a data change object to its database table.  The before and
// after model objects are marshaled to JSON prior to being stored.
func persistDataChangeEvent(ctx context.Context, db *pgxpool.Pool, tableName string, uuid uuid.UUID,
	parentUUID *uuid.UUID, before, after any) (*models.DataChangeEvent, error) {
	var err error
	var beforeJSON, afterJSON []byte
	if before != nil {
		if beforeJSON, err = json.Marshal(before); err != nil {
			return nil, fmt.Errorf("failed to marshal before object: %w", err)
		}
	}
	if after != nil {
		if afterJSON, err = json.Marshal(after); err != nil {
			return nil, fmt.Errorf("failed to marshal after object: %w", err)
		}
	}

	dataChangeEvent := models.DataChangeEvent{
		ObjectType: tableName,
		ObjectID:   uuid,
		ParentID:   parentUUID,
	}

	if beforeJSON != nil {
		value := string(beforeJSON)
		dataChangeEvent.BeforeState = &value
	}
	if afterJSON != nil {
		value := string(afterJSON)
		dataChangeEvent.AfterState = &value
	}

	result, err := utils.Create[models.DataChangeEvent](ctx, db, dataChangeEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to create data change event: %w", err)
	}

	return result, nil
}

// persistObjectWithChangeEvent persists an object to its database table and if the external API
// model representation of the object has changed then a data change event is stored.  Persisting
// of the object and its change event are captured under the same transaction to ensure we never
// lose any change events.
func persistObjectWithChangeEvent[T db.Model](ctx context.Context, db *pgxpool.Pool, record T,
	uuid uuid.UUID, parentUUID *uuid.UUID,
	converter GenericModelConverter) (*models.DataChangeEvent, error) {
	var dataChangeEvent *models.DataChangeEvent
	txCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tx, err := db.Begin(txCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	before, after, err := persistObject(ctx, db, record, uuid)
	if err != nil {
		return nil, fmt.Errorf("failed to persist object: %w", err)
	}

	afterModel := converter(*after)
	var beforeModel any = nil
	if before != nil {
		value := converter(*before)
		beforeModel = value
	}

	if beforeModel == nil || !reflect.DeepEqual(beforeModel, afterModel) {
		// Capture a change event if the data actually changed
		dataChangeEvent, err = persistDataChangeEvent(
			ctx, db, record.TableName(), uuid, parentUUID, beforeModel, afterModel)
		if err != nil {
			return nil, fmt.Errorf("failed to persist resource type data change object: %w", err)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return dataChangeEvent, nil
}

// convertMapAnyToString converts a map of any to a map of strings.  Values not of type string are
// ignored.
func convertMapAnyToString(input map[string]any) map[string]string {
	output := make(map[string]string)
	for key, value := range input {
		if _, ok := input[key].(string); ok {
			output[key] = value.(string)
		}
	}
	return output
}

// makeUUIDFromName generates a namespaced uuid value from the specified namespace and name values.  The values are
// scoped to a `cloudID` to avoid conflicts with other systems.
func makeUUIDFromName(namespace string, cloudID uuid.UUID, name string) uuid.UUID {
	value := fmt.Sprintf("%s/%s", cloudID.String(), name)
	namespaceUUID := uuid.MustParse(namespace)
	return uuid.NewSHA1(namespaceUUID, []byte(value))
}

// generateSearchApiUrl appends graphql path to the backend URL to form the fully qualified search path
func generateSearchApiUrl(backendURL string) (string, error) {
	u, err := url.Parse(backendURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse backend URL %s: %w", backendURL, err)
	}

	// Split URL address
	hostArr := strings.Split(u.Host, ".")

	// Generate search API URL
	searchUri := strings.Join(hostArr, ".")
	return fmt.Sprintf("%s://%s/searchapi/graphql", u.Scheme, searchUri), nil
}

// getClusterGraphqlVars returns the graphql variables needed to query the managed clusters
func getClusterGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	itemKind := service.KindCluster
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values:   []*string{&itemKind},
		},
	}

	return &input
}

// getNodeGraphqlVars returns the graphql variables needed to query the node instances
func getNodeGraphqlVars() *model.SearchInput {
	input := model.SearchInput{}
	kindNode := service.KindNode
	nonEmpty := "!"
	input.Filters = []*model.SearchFilter{
		{
			Property: "kind",
			Values: []*string{
				&kindNode,
			},
		},
		// Filter results without '_systemUUID' property (could happen with stale objects)
		{
			Property: "_systemUUID",
			Values:   []*string{&nonEmpty},
		},
	}
	return &input
}
