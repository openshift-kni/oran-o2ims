package persiststorage

import (
	"context"
	"errors"
	"sync"

	"github.com/openshift-kni/oran-o2ims/internal/data"
)

var ErrNotFound = errors.New("not found")

// process change function
type ProcessFunc func(dataMap *map[string]data.Object)

// interface for persistent storage
type Storage interface {
	//notification from db to application about db entry changes
	//currently assume the notification is granular to indivial entry
	ReadEntry(ctx context.Context, key string) (value string, err error)
	AddEntry(ctx context.Context, key string, value string) (err error)
	DeleteEntry(ctx context.Context, key string) (err error)
	ReadAllEntries(ctx context.Context) (result map[string]data.Object, err error)
	ProcessChanges(ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error)
	ProcessChangesWithFunction(ctx context.Context, function ProcessFunc) (err error)
}

func Add(so Storage, ctx context.Context, key string, value string) (err error) {
	return so.AddEntry(ctx, key, value)
}
func Get(so Storage, ctx context.Context, key string) (value string, err error) {
	return so.ReadEntry(ctx, key)
}
func GetAll(so Storage, ctx context.Context) (result map[string]data.Object, err error) {
	return so.ReadAllEntries(ctx)
}
func Delete(so Storage, ctx context.Context, key string) (err error) {
	return so.DeleteEntry(ctx, key)
}
func ProcessChanges(so Storage, ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error) {
	return so.ProcessChanges(ctx, dataMap, lock)
}

func ProcessChangesWithFunction(so Storage, ctx context.Context, function ProcessFunc) (err error) {
	return so.ProcessChangesWithFunction(ctx, function)
}
