package persiststorage

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

var ErrNotFound = errors.New("not found")

// ProcessFunc process change function
type ProcessFunc func(dataMap *map[string]data.Object)

// Storage interface for persistent storage
type Storage interface {
	// notification from db to application about db entry changes
	// currently assume the notification is granular to indivial entry
	ReadEntry(ctx context.Context, key string) (value string, err error)
	AddEntry(ctx context.Context, key string, value string) (err error)
	DeleteEntry(ctx context.Context, key string) (err error)
	ReadAllEntries(ctx context.Context) (result map[string]data.Object, err error)
	ProcessChanges(ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error)
	ProcessChangesWithFunction(ctx context.Context, function ProcessFunc) (err error)
}

func Add(so Storage, ctx context.Context, key, value string) (err error) {
	if err := so.AddEntry(ctx, key, value); err != nil {
		return fmt.Errorf("failed to add entry: %w", err)
	}
	return nil
}

func Get(so Storage, ctx context.Context, key string) (value string, err error) {
	value, err = so.ReadEntry(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to read entry: %w", err)
	}
	return value, nil
}

func GetAll(so Storage, ctx context.Context) (result map[string]data.Object, err error) {
	entries, err := so.ReadAllEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read all entries: %w", err)
	}
	return entries, nil
}

func Delete(so Storage, ctx context.Context, key string) (err error) {
	if err := so.DeleteEntry(ctx, key); err != nil {
		return fmt.Errorf("failed to delete entry: %w", err)
	}
	return nil
}

func ProcessChanges(so Storage, ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error) {
	if err := so.ProcessChanges(ctx, dataMap, lock); err != nil {
		return fmt.Errorf("failed to process changes: %w", err)
	}
	return nil
}

func ProcessChangesWithFunction(so Storage, ctx context.Context, function ProcessFunc) (err error) {
	if err := so.ProcessChangesWithFunction(ctx, function); err != nil {
		return fmt.Errorf("failed to process changes with function: %w", err)
	}
	return nil
}
