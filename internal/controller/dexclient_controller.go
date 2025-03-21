/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/google/uuid"

	dexv1alpha1 "github.com/rwunderer/yado/api/v1alpha1"
)

const dexclientFinalizer = "dex.capercode.eu/finalizer"

// Definitions to manage status conditions
const (
	// typeAvailableDexClient represents the status of the Deployment reconciliation
	typeAvailableDexClient = "Available"
	// typeDegradedDexClient represents the status used when the custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedDexClient = "Degraded"
)

// DexClientReconciler reconciles a DexClient object
type DexClientReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=dex.capercode.eu,resources=dexclients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dex.capercode.eu,resources=dexclients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dex.capercode.eu,resources=dexclients/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DexClient object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *DexClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the DexClient instance
	// The purpose is check if the Custom Resource for the Kind DexClient
	// is applied on the cluster if not we return nil to stop the reconciliation
	dexClient := &dexv1alpha1.DexClient{}
	err := r.Get(ctx, req.NamespacedName, dexClient)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("dexclient resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get dexclient")
		return ctrl.Result{}, err
	}

	// Let's just set the status as Unknown when no status is available
	if dexClient.Status.Conditions == nil || len(dexClient.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dexClient.Status.Conditions, metav1.Condition{Type: typeAvailableDexClient, Status: metav1.ConditionUnknown, Reason: "Reconciling", Message: "Starting reconciliation"})
		if err = r.Status().Update(ctx, dexClient); err != nil {
			log.Error(err, "Failed to update DexClient status")
			return ctrl.Result{}, err
		}

		// Let's re-fetch the dexClient Custom Resource after updating the status
		// so that we have the latest state of the resource on the cluster and we will avoid
		// raising the error "the object has been modified, please apply
		// your changes to the latest version and try again" which would re-trigger the reconciliation
		// if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, dexClient); err != nil {
			log.Error(err, "Failed to re-fetch dexClient")
			return ctrl.Result{}, err
		}
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occur before the custom resource is deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(dexClient, dexclientFinalizer) {
		log.Info("Adding Finalizer for DexClient")
		if ok := controllerutil.AddFinalizer(dexClient, dexclientFinalizer); !ok {
			log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, dexClient); err != nil {
			log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the DexClient instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isDexClientMarkedToBeDeleted := dexClient.GetDeletionTimestamp() != nil
	if isDexClientMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(dexClient, dexclientFinalizer) {
			log.Info("Performing Finalizer Operations for DexClient before delete CR")

			// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
			meta.SetStatusCondition(&dexClient.Status.Conditions, metav1.Condition{Type: typeDegradedDexClient,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("Performing finalizer operations for the custom resource: %s ", dexClient.Name)})

			if err := r.Status().Update(ctx, dexClient); err != nil {
				log.Error(err, "Failed to update DexClient status")
				return ctrl.Result{}, err
			}

			// Perform all operations required before removing the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			if err := r.doFinalizerOperationsForDexClient(dexClient); err != nil {
				log.Error(err, "Failed to do finalizer for DexClient")
				return ctrl.Result{Requeue: true}, nil
			}

			// Re-fetch the dexClient Custom Resource before updating the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raising the error "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, dexClient); err != nil {
				log.Error(err, "Failed to re-fetch dexClient")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&dexClient.Status.Conditions, metav1.Condition{Type: typeDegradedDexClient,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("Finalizer operations for custom resource %s name were successfully accomplished", dexClient.Name)})

			if err := r.Status().Update(ctx, dexClient); err != nil {
				log.Error(err, "Failed to update DexClient status")
				return ctrl.Result{}, err
			}

			log.Info("Removing Finalizer for DexClient after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(dexClient, dexclientFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for DexClient")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, dexClient); err != nil {
				log.Error(err, "Failed to remove finalizer for DexClient")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Check if the secret already exists, if not create a new one
	clientSecretNamespaceName := types.NamespacedName{
		Namespace: dexClient.Namespace,
		Name:      dexClient.Spec.SecretName,
	}
	clientSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clientSecretNamespaceName.Name,
			Namespace: clientSecretNamespaceName.Namespace,
		},
	}

	err = r.Get(ctx, clientSecretNamespaceName, &clientSecret)
	if err != nil && apierrors.IsNotFound(err) {
		// Create a new secret
		err := r.secretForDexClient(dexClient, &clientSecret)
		if err != nil {
			log.Error(err, "Failed to define new Secret resource for DexClient")

			// The following implementation will update the status
			meta.SetStatusCondition(&dexClient.Status.Conditions, metav1.Condition{Type: typeAvailableDexClient,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("Failed to create Secret for the custom resource (%s): (%s)", dexClient.Name, err)})

			if err := r.Status().Update(ctx, dexClient); err != nil {
				log.Error(err, "Failed to update DexClient status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("Creating a new Secret",
			"Secret.Namespace", clientSecret.Namespace, "Secret.Name", clientSecret.Name)
		if err = r.Create(ctx, &clientSecret); err != nil {
			log.Error(err, "Failed to create new Secret",
				"Secret.Namespace", clientSecret.Namespace, "Secret.Name", clientSecret.Name)
			return ctrl.Result{}, err
		}

		// Secret created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Secret")
		// Let's return the error for the reconciliation be re-trigged again
		return ctrl.Result{}, err
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&dexClient.Status.Conditions, metav1.Condition{Type: typeAvailableDexClient,
		Status: metav1.ConditionTrue, Reason: "Reconciled",
		Message: fmt.Sprintf("DexClient %s created successfully", dexClient.Name)})

	if err := r.Status().Update(ctx, dexClient); err != nil {
		log.Error(err, "Failed to update DexClient status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// finalizeDexClient will perform the required operations before delete the CR.
func (r *DexClientReconciler) doFinalizerOperationsForDexClient(cr *dexv1alpha1.DexClient) error {
	// TODO(user): Add the cleanup steps that the operator
	// needs to do before the CR can be deleted. Examples
	// of finalizers include performing backups and deleting
	// resources that are not owned by this CR, like a PVC.

	// Note: It is not recommended to use finalizers with the purpose of deleting resources which are
	// created and managed in the reconciliation. These ones, such as the Deployment created on this reconcile,
	// are defined as dependent of the custom resource. See that we use the method ctrl.SetControllerReference.
	// to set the ownerRef which means that the Deployment will be deleted by the Kubernetes API.
	// More info: https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))

	return nil
}

// secretForDexClient returns a Secret Object
func (r *DexClientReconciler) secretForDexClient(
	dexClient *dexv1alpha1.DexClient, clientSecret *corev1.Secret) error {

	clientSecret.StringData = map[string]string{
		"id":     uuid.New().String(),
		"secret": generateRandomString(32),
	}

	if err := ctrl.SetControllerReference(dexClient, clientSecret, r.Scheme); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DexClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dexv1alpha1.DexClient{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func generateRandomString(length int) string {
	buffer := make([]byte, length)
	_, _ = rand.Read(buffer)
	return base64.RawStdEncoding.EncodeToString(buffer)
}
