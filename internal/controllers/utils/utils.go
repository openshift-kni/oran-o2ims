package utils

import (
	"context"
	"reflect"

	ctrl "sigs.k8s.io/controller-runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var oranUtilsLog = ctrl.Log.WithName("oranUtilsLog")

func CreateK8sCR(ctx context.Context, c client.Client, Name string, Namespace string,
	newObject client.Object, ownerObject client.Object, oldObject client.Object,
	runtimeScheme *runtime.Scheme, operation string) (err error) {

	oranUtilsLog.Info("[CreateK8sCR] Resource", "name", Name)
	// Set owner reference.
	if err = controllerutil.SetControllerReference(ownerObject, newObject, runtimeScheme); err != nil {
		return err
	}

	// Check if the CR already exists.
	err = c.Get(ctx, types.NamespacedName{Name: Name, Namespace: Namespace}, oldObject)

	// If there was an error obtaining the CR and the error was "Not found", create the object.
	// If any other other occurred, return the error.
	// If the CR already exists, patch it or update it.
	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info("[CreateK8sCR] CR not found, CREATE it")
			err = c.Create(ctx, newObject)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		newObject.SetResourceVersion(oldObject.GetResourceVersion())
		if operation == PATCH {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, PATCH it")
			return c.Patch(ctx, oldObject, client.MergeFrom(newObject))
		} else if operation == UPDATE {
			oranUtilsLog.Info("[CreateK8sCR] CR already present, UPDATE it")
			return c.Update(ctx, newObject)
		}
	}

	return nil
}

func DoesK8SResourceExist(ctx context.Context, c client.Client, Name string, Namespace string, obj client.Object) (resourceExists bool, err error) {

	err = c.Get(ctx, types.NamespacedName{Name: Name, Namespace: Namespace}, obj)

	if err != nil {
		if errors.IsNotFound(err) {
			oranUtilsLog.Info("[doesK8SResourceExist] Resource not found, create it. ",
				"Type: ", reflect.TypeOf(obj), "Name: ", Name, "Namespace: ", Namespace)
			return false, nil
		} else {
			return false, err
		}
	} else {
		oranUtilsLog.Info("[doesK8SResourceExist] Resource already present, return. ",
			"Type: ", reflect.TypeOf(obj), "Name: ", Name, "Namespace: ", Namespace)
		return true, nil
	}
}
