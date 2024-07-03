package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oranv1alpha1 "github.com/openshift-kni/oran-o2ims/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = DescribeTable(
	"Reconciler",
	func(objs []client.Object, request reconcile.Request, validate func(result ctrl.Result, reconciler ClusterTemplateReconciler)) {
		// Declare the Namespace for the ClusterTemplate resource.
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-template",
			},
		}

		// Update the testcase objects to include the Namespace.
		objs = append(objs, ns)

		// Get the fake client.
		fakeClient, err := getFakeClientFromObjects(objs...)
		Expect(err).ToNot(HaveOccurred())

		// Initialize the O-RAN O2IMS reconciler.
		r := &ClusterTemplateReconciler{
			Client: fakeClient,
			Logger: logger,
		}

		// Reconcile.
		result, err := r.Reconcile(context.TODO(), request)
		Expect(err).ToNot(HaveOccurred())

		validate(result, *r)
	},
	Entry(
		"ClusterTemplate object is created and status is invalid",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: fmt.Sprintf(
						".metadata.labels[\"name\"] as $name |\n" +
							"{\n" +
							"  name: $name,\n" +
							"  alias: $name\n" +
							"}\n"),
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "cluster-template-1",
				Name:      "cluster-template-1",
			},
		},
		func(result ctrl.Result, reconciler ClusterTemplateReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 30 * time.Second}))

			// Get the ClusterTemplate and run the reconciliation once.
			clusterTemplate := &oranv1alpha1.ClusterTemplate{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				clusterTemplate)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateIsValid).
				To(Equal(false))
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateError).
				To(ContainSubstring("invalid character '.' looking for beginning of value"))
		},
	),

	Entry(
		"ClusterTemplate object is created and status is valid",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: fmt.Sprintf(
						"{\n" +
							"\"key1\": \"value1\",\n" +
							"\"price\": 2\n" +
							"}\n"),
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "cluster-template-1",
				Name:      "cluster-template-1",
			},
		},
		func(result ctrl.Result, reconciler ClusterTemplateReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Create the ClusterTemplate and run the reconciliation once.
			clusterTemplate := &oranv1alpha1.ClusterTemplate{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				clusterTemplate)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateIsValid).
				To(Equal(true))
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateError).
				To(BeEmpty())
		},
	),

	Entry(
		"ClusterTemplate object is initially invalid and then valid",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: fmt.Sprintf(
						".metadata.labels[\"name\"] as $name |\n" +
							"{\n" +
							"  name: $name,\n" +
							"  alias: $name\n" +
							"}\n"),
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "cluster-template-1",
				Name:      "cluster-template-1",
			},
		},
		func(result ctrl.Result, reconciler ClusterTemplateReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 30 * time.Second}))

			// Create the ClusterTemplate and run the reconciliation once.
			clusterTemplate := &oranv1alpha1.ClusterTemplate{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				clusterTemplate)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateIsValid).
				To(Equal(false))
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateError).
				To(ContainSubstring("invalid character '.' looking for beginning of value"))

			// Update the clusterTemplate inputDataSchema to a valid JSON schema.
			clusterTemplate.Spec.InputDataSchema = fmt.Sprintf(
				"{\n" +
					"\"key1\": \"value1\",\n" +
					"\"price\": 2\n" +
					"}\n")
			err = reconciler.Client.Update(context.TODO(), clusterTemplate)
			Expect(err).ToNot(HaveOccurred())

			// Run the reconciliation again.
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "cluster-template-1",
					Name:      "cluster-template-1",
				},
			}
			_, err = reconciler.Reconcile(context.TODO(), req)
			Expect(err).ToNot(HaveOccurred())

			// Get the ClusterTemplate again.
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-template-1",
					Namespace: "cluster-template-1",
				},
				clusterTemplate)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateIsValid).
				To(Equal(true))
			Expect(clusterTemplate.Status.ClusterTemplateValidation.ClusterTemplateError).
				To(BeEmpty())
		},
	),
)
