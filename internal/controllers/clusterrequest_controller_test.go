package controllers

import (
	"context"
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
	func(objs []client.Object, request reconcile.Request,
		validate func(result ctrl.Result, reconciler ClusterRequestReconciler)) {

		// Declare the Namespace for the ClusterRequest resource.
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
		r := &ClusterRequestReconciler{
			Client: fakeClient,
			Logger: logger,
		}

		// Reconcile.
		result, err := r.Reconcile(context.TODO(), request)
		Expect(err).ToNot(HaveOccurred())

		validate(result, *r)
	},
	Entry(
		"ClusterRequest input matches ClusterTemplate",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: `{
							"type": "object",
							"properties": {
								"name": {
									"type": "string"
								},
								"age": {
									"type": "integer"
								},
								"email": {
									"type": "string",
									"format": "email"
								},
								"address": {
									"type": "object",
									"properties": {
										"street": {
											"type": "string"
										},
										"city": {
											"type": "string"
										},
										"zipcode": {
											"type": "string"
										},
										"capital": {
										  "type": "boolean"
										}
									},
									"required": ["street", "city"]
								},
								"phoneNumbers": {
									"type": "array",
									"items": {
										"type": "string"
									}
								}
							},
							"required": ["name", "age"]
						  }`,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: `{
						"name": "Bob",
						"age": 35,
						"email": "bob@example.com",
						"phoneNumbers": ["123-456-7890", "987-654-3210"]
					  }`,
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(true))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
				To(Equal(true))
		},
	),

	Entry(
		"ClusterRequest input does not match ClusterTemplate",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: `{
							"type": "object",
							"properties": {
								"name": {
									"type": "string"
								},
								"age": {
									"type": "integer"
								},
								"email": {
									"type": "string",
									"format": "email"
								},
								"address": {
									"type": "object",
									"properties": {
										"street": {
											"type": "string"
										},
										"city": {
											"type": "string"
										},
										"zipcode": {
											"type": "string"
										},
										"capital": {
										  "type": "boolean"
										}
									},
									"required": ["street", "city"]
								},
								"phoneNumbers": {
									"type": "array",
									"items": {
										"type": "string"
									}
								}
							},
							"required": ["name", "age", "address"]
						  }`,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: `{
						"name": "Bob",
						"age": 35,
						"email": "bob@example.com",
						"phoneNumbers": ["123-456-7890", "987-654-3210"]
					  }`,
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 30 * time.Second}))

			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(true))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
				To(Equal(false))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
				To(ContainSubstring("The JSON input does not match the JSON schema:  (root): address is required"))
		},
	),

	Entry(
		"ClusterTemplate specified by ClusterTemplateRef is missing and input is invalid",
		[]client.Object{
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: `
						"name": "Bob",
						"age": 35,
						"email": "bob@example.com",
						"phoneNumbers": ["123-456-7890", "987-654-3210"]
					  }`,
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 30 * time.Second}))

			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(false))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
				To(Equal(false))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
				To(ContainSubstring("The referenced ClusterTemplate (cluster-template) does not exist in the cluster-template namespace"))
		},
	),

	Entry(
		"ClusterTemplate change triggers automatic reconciliation",
		[]client.Object{
			&oranv1alpha1.ClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-template",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterTemplateSpec{
					InputDataSchema: `{
							"type": "object",
							"properties": {
								"name": {
									"type": "string"
								},
								"age": {
									"type": "integer"
								},
								"email": {
									"type": "string",
									"format": "email"
								},
								"address": {
									"type": "object",
									"properties": {
										"street": {
											"type": "string"
										},
										"city": {
											"type": "string"
										},
										"zipcode": {
											"type": "string"
										},
										"capital": {
										  "type": "boolean"
										}
									},
									"required": ["street", "city"]
								},
								"phoneNumbers": {
									"type": "array",
									"items": {
										"type": "string"
									}
								}
							},
							"required": ["name", "age"]
						  }`,
				},
			},
			&oranv1alpha1.ClusterRequest{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				Spec: oranv1alpha1.ClusterRequestSpec{
					ClusterTemplateRef: "cluster-template",
					ClusterTemplateInput: `{
						"name": "Bob",
						"age": 35,
						"email": "bob@example.com",
						"phoneNumbers": ["123-456-7890", "987-654-3210"]
					  }`,
				},
			},
		},
		reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "cluster-request",
				Namespace: "cluster-template",
			},
		},
		func(result ctrl.Result, reconciler ClusterRequestReconciler) {
			Expect(result).To(Equal(ctrl.Result{RequeueAfter: 5 * time.Minute}))

			// Get the ClusterRequest and check that everything is valid.
			clusterRequest := &oranv1alpha1.ClusterRequest{}
			err := reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(true))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
				To(Equal(true))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
				To(Equal(""))

			// Update the ClusterTemplate to have the address required.
			// Get the ClusterRequest again.
			clusterTemplate := &oranv1alpha1.ClusterTemplate{}
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-template",
					Namespace: "cluster-template",
				},
				clusterTemplate)
			Expect(err).ToNot(HaveOccurred())

			clusterTemplate.Spec.InputDataSchema =
				`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					},
					"age": {
						"type": "integer"
					},
					"email": {
						"type": "string",
						"format": "email"
					},
					"address": {
						"type": "object",
						"properties": {
							"street": {
								"type": "string"
							},
							"city": {
								"type": "string"
							},
							"zipcode": {
								"type": "string"
							},
							"capital": {
							  "type": "boolean"
							}
						},
						"required": ["street", "city"]
					},
					"phoneNumbers": {
						"type": "array",
						"items": {
							"type": "string"
						}
					}
				},
				"required": ["name", "age", "address"]
			}`

			err = reconciler.Client.Update(context.TODO(), clusterTemplate)
			Expect(err).ToNot(HaveOccurred())

			// The reconciliation doesn't run automatically here, but we can obtain it
			// from the findClusterRequestsForClusterTemplate function and run it.
			req := reconciler.findClusterRequestsForClusterTemplate(context.TODO(), clusterTemplate)
			Expect(req).To(HaveLen(1))
			_, err = reconciler.Reconcile(context.TODO(), req[0])
			Expect(err).ToNot(HaveOccurred())

			// Get the ClusterRequest again.
			err = reconciler.Client.Get(
				context.TODO(),
				types.NamespacedName{
					Name:      "cluster-request",
					Namespace: "cluster-template",
				},
				clusterRequest)
			Expect(err).ToNot(HaveOccurred())

			// Expect for the ClusterRequest to not match the ClusterTemplate and to
			// report that the required field is missing.
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputIsValid).
				To(Equal(true))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplate).
				To(Equal(false))
			Expect(clusterRequest.Status.ClusterTemplateInputValidation.InputMatchesTemplateError).
				To(ContainSubstring("The JSON input does not match the JSON schema:  (root): address is required"))
		},
	),
)
