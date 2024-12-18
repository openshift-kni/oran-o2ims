package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openshift-kni/oran-o2ims/internal/service/common/db"
	"github.com/openshift-kni/oran-o2ims/internal/service/common/db/models"
)

// GenericModelConverter converts a db model object into an API object and returns it as an `any`
// so that it can be used generically since the API model doesn't implement a base interface.
type GenericModelConverter func(object interface{}) any

// PersistObject persists an object to its database table.  If the object does not already have a
// persisted representation then it is created; otherwise any modified fields are updated in the
// database tuple.  The function returns both the before and after versions of the object.
func PersistObject[T db.Model](ctx context.Context, tx pgx.Tx,
	object T, uuid uuid.UUID) (*T, *T, error) {
	var before, after *T
	// Store the object into the database handling cases for both insert/update separately so that we have access to the
	// before & after view of the data.
	var record, err = Find[T](ctx, tx, uuid)
	if errors.Is(err, ErrNotFound) {
		// New object instance
		after, err = Create[T](ctx, tx, object)
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
	tags := CompareObjects(*record, object, "CreatedAt")
	if len(tags) == 0 {
		// This shouldn't happen since the generation id always changes
		after = before
		slog.Warn("no change detected on persisted object", "table", object.TableName(), "uuid", uuid)
		return before, after, nil
	}

	after, err = Update[T](ctx, tx, uuid, object, tags.Fields()...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update object '%s/%s': %w", object.TableName(), uuid, err)
	}

	slog.Debug("object updated",
		"table", object.TableName(), "uuid", uuid, "before", before, "after", after, "columns", tags.Fields())

	return before, after, nil
}

// PersistDataChangeEvent persists a data change object to its database table.  The before and
// after model objects are marshaled to JSON prior to being stored.
func PersistDataChangeEvent(ctx context.Context, tx pgx.Tx, tableName string, uuid uuid.UUID,
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

	result, err := Create[models.DataChangeEvent](ctx, tx, dataChangeEvent)
	if err != nil {
		return nil, fmt.Errorf("failed to create data change event: %w", err)
	}

	return result, nil
}

// Rollback attempts to execute a Rollback on a transaction.  It is safe to invoke this as a
// deferred function call even if the transaction has already been committed.
func Rollback(ctx context.Context, tx pgx.Tx) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		slog.Error("failed to rollback transaction", "error", err)
	}
}

// PersistObjectWithChangeEvent persists an object to its database table and if the external API
// model representation of the object has changed then a data change event is stored.  Persisting
// of the object and its change event are captured under the same transaction to ensure we never
// lose any change events.
func PersistObjectWithChangeEvent[T db.Model](ctx context.Context, db *pgxpool.Pool, record T,
	uuid uuid.UUID, parentUUID *uuid.UUID,
	converter GenericModelConverter) (*models.DataChangeEvent, error) {
	var dataChangeEvent *models.DataChangeEvent

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer Rollback(ctx, tx)

	before, after, err := PersistObject(ctx, tx, record, uuid)
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
		dataChangeEvent, err = PersistDataChangeEvent(
			ctx, tx, record.TableName(), uuid, parentUUID, beforeModel, afterModel)
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
