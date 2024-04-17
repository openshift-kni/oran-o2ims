package persiststorage

import (
	"context"
	"errors"
	"sync"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

var ErrNotFound = errors.New("not found")

// interface for persistent storage
type StorageOperations interface {
	//notification from db to application about db entry changes
	//currently assume the notification is granular to indivial entry
	ReadEntry(ctx context.Context, key string) (value string, err error)
	AddEntry(ctx context.Context, key string, value string) (err error)
	DeleteEntry(ctx context.Context, key string) (err error)
	ReadAllEntries(ctx context.Context) (result map[string]data.Object, err error)
	ProcessChanges(ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error)
}

func Add(so StorageOperations, ctx context.Context, key string, value string) (err error) {
	return so.AddEntry(ctx, key, value)
}
func Get(so StorageOperations, ctx context.Context, key string) (value string, err error) {
	return so.ReadEntry(ctx, key)
}
func GetAll(so StorageOperations, ctx context.Context) (result map[string]data.Object, err error) {
	return so.ReadAllEntries(ctx)
}
func Delete(so StorageOperations, ctx context.Context, key string) (err error) {
	return so.DeleteEntry(ctx, key)
}
func ProcessChanges(so StorageOperations, ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error) {
	return so.ProcessChanges(ctx, dataMap, lock)
}
