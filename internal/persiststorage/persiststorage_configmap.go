package persiststorage

import (
	"context"
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

type KubeConfigMapStore struct {
	nameSpace  string
	name       string
	fieldOwner string
	jsonAPI    *jsoniter.API
	client     *k8s.Client
}

func NewKubeConfigMapStore() *KubeConfigMapStore {
	return &KubeConfigMapStore{}
}

func (b *KubeConfigMapStore) SetNameSpace(
	ns string) *KubeConfigMapStore {
	b.nameSpace = ns
	return b
}
func (b *KubeConfigMapStore) SetName(
	name string) *KubeConfigMapStore {
	b.name = name
	return b
}

func (b *KubeConfigMapStore) SetFieldOwnder(
	owner string) *KubeConfigMapStore {
	b.fieldOwner = owner
	return b
}

func (b *KubeConfigMapStore) SetJsonAPI(
	jsonAPI *jsoniter.API) *KubeConfigMapStore {
	b.jsonAPI = jsonAPI
	return b
}

func (b *KubeConfigMapStore) SetClient(
	client *k8s.Client) *KubeConfigMapStore {
	b.client = client
	return b
}

// k8s configmap methods
func (s *KubeConfigMapStore) AddEntry(ctx context.Context, entryKey string, value string) (err error) {
	//test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.nameSpace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		return
	}

	savedData := configmap.Data

	//configmap does not exist
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
			Namespace: s.nameSpace,
			Name:      s.name,
		},
		Data: savedData,
	}

	err = (*s.client).Patch(ctx, configmap, clnt.Apply, clnt.ForceOwnership, clnt.FieldOwner(s.fieldOwner))

	return
}

func (s *KubeConfigMapStore) DeleteEntry(ctx context.Context, entryKey string) (err error) {
	//test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.nameSpace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		//there is error and err is not notfound error
		//panic("unexpected error")
		return
	}

	if configmap.Data == nil {
		return
	}

	_, ok := configmap.Data[entryKey]

	//entry not found
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
			Namespace: s.nameSpace,
			Name:      s.name,
		},
		Data: configmap.Data,
	}
	// Note: there is a case configmap is empty shall we remove the configmap
	err = (*s.client).Patch(ctx, configmap, clnt.Apply, clnt.ForceOwnership, clnt.FieldOwner(s.fieldOwner))
	return
}

func (s *KubeConfigMapStore) ReadEntry(ctx context.Context, entryKey string) (value string, err error) {

	//test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.nameSpace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		//there is error and err is not notfound error
		//panic("unexpected error")
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

	//test to read the configmap
	configmap := &corev1.ConfigMap{}

	key := clnt.ObjectKey{
		Namespace: s.nameSpace,
		Name:      s.name,
	}
	err = (*s.client).Get(ctx, key, configmap)

	if err != nil && !apierrors.IsNotFound(err) {
		//there is error and err is not notfound error
		//panic("unexpected error")
		return
	}

	err = nil

	if configmap.Data == nil {
		return
	}

	for mapKey, value := range configmap.Data {
		var object data.Object
		err = (*s.jsonAPI).Unmarshal([]byte(value), &object)
		if err != nil {
			continue
		}
		result[mapKey] = object
	}
	return
}

func (s *KubeConfigMapStore) ProcessChanges(ctx context.Context, dataMap **map[string]data.Object, lock *sync.Mutex) (err error) {
	raw_opt := metav1.SingleObject(metav1.ObjectMeta{
		Namespace: s.nameSpace,
		Name:      s.name,
	})
	opt := clnt.ListOptions{}
	opt.Raw = &raw_opt

	watcher, err := s.client.Watch(ctx, &corev1.ConfigMapList{}, &opt)

	go func() {
		for {
			event, open := <-watcher.ResultChan()
			if open {
				switch event.Type {
				case watch.Added, watch.Modified:
					configmap, _ := event.Object.(*corev1.ConfigMap)
					newMap := map[string]data.Object{}
					for k, value := range configmap.Data {
						var object data.Object
						err = (*s.jsonAPI).Unmarshal([]byte(value), &object)
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
		}
	}()

	return
}
