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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	harborconfigurationv1alpha1 "github.com/giantswarm/harbor-config-operator/api/v1alpha1"
	harborOperator "github.com/goharbor/harbor-operator/apis/goharbor.io/v1beta1"
	"github.com/goharbor/harbor-operator/pkg/cluster/k8s"
	apiv2 "github.com/mittwald/goharbor-client/v5/apiv2"
	modelv2 "github.com/mittwald/goharbor-client/v5/apiv2/model"
	harborerrors "github.com/mittwald/goharbor-client/v5/apiv2/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	harborClusterGVM = schema.GroupVersionResource{
		Group:    "goharbor.io",
		Version:  "v1alpha3",
		Resource: "harborclusters",
	}
)

// HarborConfigurationReconciler reconciles a HarborConfiguration object
type HarborConfigurationReconciler struct {
	DClient *k8s.DynamicClientWrapper
	client.Client
	*runtime.Scheme
}

//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=harbor-configuration.harbor.configuration,resources=harborconfigurations/finalizers,verbs=update
//+kubebuilder:rbac:groups=harborclusters.goharbor.io,resources=harborclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=harborclusters.goharbor.io,resources=harborclusters/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=harborclusters.goharbor.io,resources=harborclusters/finalizers,verbs=get;list;watch;create;update;patch;delete

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

	var harborConfiguration harborconfigurationv1alpha1.HarborConfiguration

	err := r.Get(ctx, req.NamespacedName, &harborConfiguration)
	if err != nil {
		return ctrl.Result{}, err
	}

	var harborTarget harborOperator.HarborCluster

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("error getting user home dir: %v\n", err)
		os.Exit(1)
	}
	kubeConfigPath := filepath.Join(userHomeDir, ".kube", "config")
	fmt.Printf("Using kubeconfig: %s\n", kubeConfigPath)

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		fmt.Printf("error getting Kubernetes config: %v\n", err)
		os.Exit(1)
	}

	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		fmt.Printf("error creating dynamic client: %v\n", err)
		os.Exit(1)
	}

	crdClient := dynamicClient.Resource(harborClusterGVM).Namespace(harborConfiguration.Spec.HarborTarget.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	harborUnstructured, err := crdClient.Get(ctx, harborConfiguration.Spec.HarborTarget.Name, v1.GetOptions{
		TypeMeta: v1.TypeMeta{
			Kind:       "HarborCluster",
			APIVersion: "v1alpha3",
		},
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(harborUnstructured.UnstructuredContent(), &harborTarget)
	if err != nil {
		return ctrl.Result{}, err
	}

	client, err := apiv2.NewRESTClientForHost(harborTarget.Spec.ExternalURL, "admin", harborTarget.Spec.HarborAdminPasswordRef, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	registry := &modelv2.Registry{
		Name:        harborConfiguration.Spec.Registry.Name,
		Type:        harborConfiguration.Spec.Registry.Type,
		URL:         harborConfiguration.Spec.Registry.TargetRegistryUrl,
		Description: harborConfiguration.Spec.Registry.Description,
		Credential:  (*modelv2.RegistryCredential)(harborConfiguration.Spec.Registry.Credential),
	}
	r.registryReconciliation(ctx, harborConfiguration, *registry, client)

	r.projectReconciliation(ctx, harborConfiguration, *registry, client)

	r.replicationRuleReconciliation(ctx, harborConfiguration, *registry, client)

	// if harborConfiguration.ObjectMeta.DeletionTimestamp.IsZero() {
	// } else {
	// 	err = client.DeleteRegistryByID(ctx, harborConfiguration.Status.RegistryId)
	// 	if err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// }

	// err = client.DeleteProject(ctx, harborConfiguration.Status.ProjectId)
	// if err != nil {
	// 	return ctrl.Result{}, err
	// }

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

func (r *HarborConfigurationReconciler) registryReconciliation(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, registry modelv2.Registry, client *apiv2.RESTClient) (ctrl.Result, error) {
	srcRegistry, err := client.GetRegistryByName(ctx, registry.Name)
	hErr := &harborerrors.ErrRegistryNotFound{}

	if err != nil && errors.Is(err, hErr) {
		err = client.NewRegistry(ctx, &registry)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err == nil {
		update := &modelv2.RegistryUpdate{
			Name:        &harborConfiguration.Spec.Registry.Name,
			URL:         &harborConfiguration.Spec.Registry.TargetRegistryUrl,
			Description: &harborConfiguration.Spec.Registry.Description,
		}
		if harborConfiguration.Spec.Registry.Credential != nil {
			update.AccessKey = &harborConfiguration.Spec.Registry.Credential.AccessKey
			update.AccessSecret = &harborConfiguration.Spec.Registry.Credential.AccessSecret
			update.CredentialType = &harborConfiguration.Spec.Registry.Credential.Type
		}
		err = client.UpdateRegistry(ctx, update, srcRegistry.ID)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *HarborConfigurationReconciler) projectReconciliation(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, registry modelv2.Registry, client *apiv2.RESTClient) (ctrl.Result, error) {

	srcRegistry, err := client.GetRegistryByName(ctx, registry.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	project := &modelv2.ProjectReq{
		ProjectName:  harborConfiguration.Spec.ProjectReq.ProjectName,
		Public:       harborConfiguration.Spec.ProjectReq.Public,
		StorageLimit: harborConfiguration.Spec.ProjectReq.StorageLimit,
		RegistryID:   &srcRegistry.ID,
	}

	_, err = client.GetProject(ctx, project.ProjectName)
	hErr := &harborerrors.ErrProjectNotFound{}

	if err != nil && errors.Is(err, hErr) {
		err = client.NewProject(ctx, project)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err == nil {
		// Probably dead code as you cant edit project in UI
		update := &modelv2.Project{
			Name:       harborConfiguration.Spec.ProjectReq.ProjectName,
			RegistryID: srcRegistry.ID,
		}
		err = client.UpdateProject(ctx, update, project.StorageLimit)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, err
}

func (r *HarborConfigurationReconciler) replicationRuleReconciliation(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, registry modelv2.Registry, client *apiv2.RESTClient) (ctrl.Result, error) {
	srcRegistry, err := client.GetRegistryByName(ctx, registry.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	reqFilters := make([]*modelv2.ReplicationFilter, 0)
	for _, v := range harborConfiguration.Spec.Replication.Filters {
		temp := modelv2.ReplicationFilter{}
		err := json.Unmarshal(v.Raw, &temp)
		if err != nil {
			return ctrl.Result{}, err
		}
		reqFilters = append(reqFilters, &temp)
	}

	var reqDestinationRegistry *modelv2.Registry
	if harborConfiguration.Spec.Replication.DestinationRegistry != nil {
		err = json.Unmarshal(harborConfiguration.Spec.Replication.DestinationRegistry.Raw, reqDestinationRegistry)
		if err != nil {
			return ctrl.Result{}, err
		}

	}

	var reqTrigger *modelv2.ReplicationTrigger
	if harborConfiguration.Spec.Replication.Trigger != nil {
		err = json.Unmarshal(harborConfiguration.Spec.Replication.Trigger.Raw, &reqTrigger)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	hErr := &harborerrors.ErrNotFound{}
	replicationFound, err := client.GetReplicationPolicyByName(ctx, harborConfiguration.Spec.Replication.Name)

	if err != nil && errors.Is(err, hErr) {
		err = client.NewReplicationPolicy(ctx,
			reqDestinationRegistry,
			srcRegistry,
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
	} else if err == nil {
		update := modelv2.ReplicationPolicy{
			Name:          harborConfiguration.Spec.Replication.Name,
			Description:   harborConfiguration.Spec.Replication.Description,
			SrcRegistry:   srcRegistry,
			DestNamespace: harborConfiguration.Spec.Replication.DestinationNamespace,
			DestRegistry:  reqDestinationRegistry,
			Filters:       reqFilters,
			Trigger:       reqTrigger,
			ID:            replicationFound.ID,
			Override:      harborConfiguration.Spec.Replication.Override,
			Enabled:       harborConfiguration.Spec.Replication.EnablePolicy,
		}

		err = client.UpdateReplicationPolicy(ctx, &update, replicationFound.ID)
		// Experienced some flakiness
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		return ctrl.Result{}, err
	}

	replicationFound, err = client.GetReplicationPolicyByName(ctx, harborConfiguration.Spec.Replication.Name)
	if err != nil {
		return ctrl.Result{}, err
	}

	trigger := &modelv2.StartReplicationExecution{
		PolicyID: replicationFound.ID,
	}
	err = client.TriggerReplicationExecution(ctx, trigger)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
}
