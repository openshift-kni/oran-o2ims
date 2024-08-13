package persiststorage

import (
	"context"
	"errors"
	"sync"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-kni/oran-o2ims/internal/data"
	"github.com/openshift-kni/oran-o2ims/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
)

type KubeConfigMapStoreBuilder struct {
	namespace  string
	name       string
	fieldOwner string
	jsonAPI    jsoniter.API
	client     *k8s.Client
}

func NewKubeConfigMapStoreBuilder() *KubeConfigMapStoreBuilder {
	return &KubeConfigMapStoreBuilder{}
}

func (b *KubeConfigMapStoreBuilder) SetNamespace(
	namespace string) *KubeConfigMapStoreBuilder {
	b.namespace = namespace
	return b
}

func (b *KubeConfigMapStoreBuilder) SetName(
	name string) *KubeConfigMapStoreBuilder {
	b.name = name
	return b
}

func (b *KubeConfigMapStoreBuilder) SetFieldOwner(
	fieldOwner string) *KubeConfigMapStoreBuilder {
	b.fieldOwner = fieldOwner
	return b
}

func (b *KubeConfigMapStoreBuilder) SetJsonAPI(
	api jsoniter.API) *KubeConfigMapStoreBuilder {
	b.jsonAPI = api
	return b
}

func (b *KubeConfigMapStoreBuilder) SetClient(
	client *k8s.Client) *KubeConfigMapStoreBuilder {
	b.client = client
	return b
}
func (b *KubeConfigMapStoreBuilder) Build() (storage *KubeConfigMapStore,
	err error) {
	if b.namespace == "" {
		err = errors.New("namespace is empty")
		return
	}
	if b.name == "" {
		err = errors.New("name is empty")
		return
	}
	if b.fieldOwner == "" {
		err = errors.New("fieldOwner is empty")
		return
	}
	if b.client == nil {
		err = errors.New("k8s client could not be nil")
	}

	storage = &KubeConfigMapStore{
		namespace:  b.namespace,
		name:       b.name,
		fieldOwner: b.fieldOwner,
		jsonAPI:    b.jsonAPI,
		client:     b.client,
	}

	return
}

type KubeConfigMapStore struct {
	namespace  string
	name       string
	fieldOwner string
	jsonAPI    jsoniter.API
	client     *k8s.Client
}

func (s *KubeConfigMapStore) GetName() (name string) {
	return s.name
}

// k8s configmap methods
func (s *KubeConfigMapStore) AddEntry(ctx context.Context, entryKey, value string) (err error) {
	// test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.namespace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		return
	}

	savedData := configmap.Data

	// configmap does not exist
	if savedData == nil {
		savedData = map[string]string{}
	}
	savedData[entryKey] = value

	configmap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: s.namespace,
			Name:      s.name,
		},
		Data: savedData,
	}

	err = (*s.client).Patch(ctx, configmap, clnt.Apply, clnt.ForceOwnership, clnt.FieldOwner(s.fieldOwner))

	return
}

func (s *KubeConfigMapStore) DeleteEntry(ctx context.Context, entryKey string) (err error) {
	// test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.namespace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		// there is error and err is not notfound error
		// panic("unexpected error")
		return
	}

	if configmap.Data == nil {
		return
	}

	_, ok := configmap.Data[entryKey]

	// entry not found
	if !ok {
		return
	}
	delete(configmap.Data, entryKey)

	configmap = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: s.namespace,
			Name:      s.name,
		},
		Data: configmap.Data,
	}
	// Note: there is a case configmap is empty shall we remove the configmap
	err = (*s.client).Patch(ctx, configmap, clnt.Apply, clnt.ForceOwnership, clnt.FieldOwner(s.fieldOwner))
	return
}

func (s *KubeConfigMapStore) ReadEntry(ctx context.Context, entryKey string) (value string, err error) {

	// test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.namespace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		// there is error and err is not notfound error
		// panic("unexpected error")
		return
	}

	err = nil
	if configmap.Data == nil {
		return
	}

	value, ok := configmap.Data[entryKey]
	if !ok {
		err = ErrNotFound
	}
	return
}

// The handler read to DB and build/recovery datastructures in memory
func (s *KubeConfigMapStore) ReadAllEntries(ctx context.Context) (result map[string]data.Object, err error) {
	result = map[string]data.Object{}

	// test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.namespace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		// there is error and err is not notfound error
		// panic("unexpected error")
		return
	}

	err = nil

	if configmap.Data == nil {
		return
	}

	for mapKey, value := range configmap.Data {
		var object data.Object
		err = s.jsonAPI.Unmarshal([]byte(value), &object)
		if err != nil {
			continue
		}
		result[mapKey] = object
	}
	return
}

func (s *KubeConfigMapStore) ProcessChanges(ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error) {
	rawOpt := metav1.SingleObject(metav1.ObjectMeta{
		Namespace: s.namespace,
		Name:      s.name,
	})
	opt := clnt.ListOptions{}
	opt.Raw = &rawOpt

	watcher, err := s.client.Watch(ctx, &corev1.ConfigMapList{}, &opt)

	go func() {
		for {
			event := <-watcher.ResultChan()
			switch event.Type {
			case watch.Added, watch.Modified:
				configmap, _ := event.Object.(*corev1.ConfigMap)
				newMap := map[string]data.Object{}
				for k, value := range configmap.Data {
					var object data.Object
					err = s.jsonAPI.Unmarshal([]byte(value), &object)
					if err != nil {
						continue
					}
					newMap[k] = object
				}

				lock.Lock()
				*dataMap = &newMap
				lock.Unlock()
			case watch.Deleted:
				lock.Lock()
				*dataMap = &map[string]data.Object{}
				lock.Unlock()

			default:

			}
		}
	}()

	return
}

func (s *KubeConfigMapStore) ProcessChangesWithFunction(ctx context.Context, function ProcessFunc) (err error) {
	rawOpt := metav1.SingleObject(metav1.ObjectMeta{
		Namespace: s.namespace,
		Name:      s.name,
	})
	opt := clnt.ListOptions{}
	opt.Raw = &rawOpt

	watcher, err := s.client.Watch(ctx, &corev1.ConfigMapList{}, &opt)

	go func() {
		for {
			event := <-watcher.ResultChan()
			switch event.Type {
			case watch.Added, watch.Modified:
				configmap, _ := event.Object.(*corev1.ConfigMap)
				newMap := map[string]data.Object{}
				for k, value := range configmap.Data {
					var object data.Object
					err = s.jsonAPI.Unmarshal([]byte(value), &object)
					if err != nil {
						continue
					}
					newMap[k] = object
				}
				function(&newMap)

			case watch.Deleted:
				function(&map[string]data.Object{})

			default:

			}

		}
	}()

	return
}
