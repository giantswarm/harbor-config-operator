/*
Copyright 2022.

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

package controllers

import (
	"context"
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	harborconfigurationv1alpha1 "github.com/giantswarm/harbor-config-operator/api/v1alpha1"
	apiv2 "github.com/mittwald/goharbor-client/v5/apiv2"
	modelv2 "github.com/mittwald/goharbor-client/v5/apiv2/model"
)

// HarborConfigurationReconciler reconciles a HarborConfiguration object
type HarborConfigurationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HarborConfiguration object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *HarborConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	// Need to add update function and have logic to tell reconiler what to do

	var harborConfiguration harborconfigurationv1alpha1.HarborConfiguration

	err := r.Get(ctx, req.NamespacedName, &harborConfiguration)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Set target harbor cluster

	client, err := apiv2.NewRESTClientForHost(harborConfiguration.Spec.HarborTarget.ApiUrl, harborConfiguration.Spec.HarborTarget.Username, harborConfiguration.Spec.HarborTarget.Password, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Populate registry

	myRegistry := &modelv2.Registry{
		Name:        harborConfiguration.Spec.Registry.Name,
		Type:        harborConfiguration.Spec.Registry.Type,
		URL:         harborConfiguration.Spec.Registry.TargetRegistryUrl,
		Description: harborConfiguration.Spec.Registry.Description,
		Credential:  (*modelv2.RegistryCredential)(harborConfiguration.Spec.Registry.Credential),
	}

	// Create or delete registry
	// Need to update logic to tell if deletion should be called as all in same loop
	// eg if i delete a registry it shouldnt also delete my projects

	// if harborConfiguration.ObjectMeta.DeletionTimestamp.IsZero() {
	err = client.NewRegistry(ctx, myRegistry)
	if err != nil {
		return ctrl.Result{}, err
	}
	// } else {
	// 	err = client.DeleteRegistryByID(ctx, harborConfiguration.Status.RegistryId)
	// 	if err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// }

	// Create or delete project
	// Need logic to tell if deletion should be called

	registeries, err := client.ListRegistries(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	type storeReg struct {
		Registry *modelv2.Registry
	}

	var store = storeReg{}

	for _, registery := range registeries {
		if registery.Name == myRegistry.Name {
			store.Registry = registery
		}
	}

	myProject := &modelv2.ProjectReq{
		ProjectName:  harborConfiguration.Spec.ProjectReq.ProjectName,
		Public:       harborConfiguration.Spec.ProjectReq.Public,
		StorageLimit: harborConfiguration.Spec.ProjectReq.StorageLimit,
		RegistryID:   &store.Registry.ID,
	}

	err = client.NewProject(ctx, myProject)
	if err != nil {
		return ctrl.Result{}, err
	}

	// err = client.DeleteProject(ctx, harborConfiguration.Status.ProjectId)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }

	// Create or delete a replication rule
	reqFilters := make([]*modelv2.ReplicationFilter, 0)
	for _, v := range harborConfiguration.Spec.Replication.Filters {
		temp := modelv2.ReplicationFilter{}
		err := json.Unmarshal(v.Raw, &temp)
		if err != nil {
			return ctrl.Result{}, err
		}
		reqFilters = append(reqFilters, &temp)
	}

	// fixup for registry TODO IMRPOVE
	var reqDestinationRegistry *modelv2.Registry
	if harborConfiguration.Spec.Replication.DestinationRegistry != nil {
		err = json.Unmarshal(harborConfiguration.Spec.Replication.DestinationRegistry.Raw, reqDestinationRegistry)
		if err != nil {
			return ctrl.Result{}, err
		}

	}
	// fixup for trigger TODO IMRPOVE
	var reqTrigger *modelv2.ReplicationTrigger
	if harborConfiguration.Spec.Replication.Trigger != nil {
		err = json.Unmarshal(harborConfiguration.Spec.Replication.Trigger.Raw, &reqTrigger)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	err = client.NewReplicationPolicy(ctx,
		store.Registry,
		reqDestinationRegistry,
		harborConfiguration.Spec.Replication.ReplicateDeletion,
		harborConfiguration.Spec.Replication.Override,
		harborConfiguration.Spec.Replication.EnablePolicy,
		reqFilters,
		reqTrigger,
		harborConfiguration.Spec.Replication.DestinationNamespace,
		harborConfiguration.Spec.Replication.Description,
		harborConfiguration.Spec.Replication.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	// err = client.DeleteReplicationPolicyByID(ctx, harborConfiguration.Status.ReplicationId)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HarborConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&harborconfigurationv1alpha1.HarborConfiguration{}).
		Complete(r)
}
