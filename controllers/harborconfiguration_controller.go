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

	harborOperator "github.com/goharbor/harbor-operator/apis/goharbor.io/v1beta1"
	"github.com/goharbor/harbor-operator/pkg/cluster/k8s"
	apiv2 "github.com/mittwald/goharbor-client/v5/apiv2"
	modelv2 "github.com/mittwald/goharbor-client/v5/apiv2/model"
	harborerrors "github.com/mittwald/goharbor-client/v5/apiv2/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	harborconfigurationv1alpha1 "github.com/giantswarm/harbor-config-operator/api/v1alpha1"
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

//+kubebuilder:rbac:groups=administration.harbor.configuration,resources=harborconfigurations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=administration.harbor.configuration,resources=harborconfigurations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=administration.harbor.configuration,resources=harborconfigurations/finalizers,verbs=update
//+kubebuilder:rbac:groups=goharbor.io,resources=harborclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=goharbor.io,resources=harborclusters/status,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=goharbor.io,resources=harborclusters/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets;services,verbs=get;list

func (r *HarborConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	var harborConfiguration harborconfigurationv1alpha1.HarborConfiguration
	err := r.Get(ctx, req.NamespacedName, &harborConfiguration)
	if err != nil {
		return ctrl.Result{}, err
	}

	dynamicClient, err := getDynamicKubeConfig()
	if err != nil {
		return ctrl.Result{}, err
	}

	crdClient := dynamicClient.Resource(harborClusterGVM).Namespace(harborConfiguration.Spec.HarborTarget.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}

	var harborTarget harborOperator.HarborCluster
	harborTarget, err = getConcreteHarborType(ctx, crdClient, harborConfiguration, harborTarget)
	if err != nil {
		return ctrl.Result{}, err
	}

	clientSet, err := getTypedKubeConfig()
	if err != nil {
		return ctrl.Result{}, err
	}

	haborSecret, err := getHarborSecret(ctx, clientSet, &harborTarget)
	if err != nil {
		return ctrl.Result{}, err
	}

	client, err := apiv2.NewRESTClientForHost(getHarborURL(&harborTarget), harborConfiguration.Spec.HarborTarget.HarborUsername, haborSecret, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	if harborConfiguration.ObjectMeta.DeletionTimestamp.IsZero() {
		_, err = r.reconcileAll(ctx, harborConfiguration, client)
		if err != nil {
			return ctrl.Result{}, err
		}

		_, err = triggerReplication(ctx, harborConfiguration, client)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		_, err = deleteAll(ctx, harborConfiguration, client)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HarborConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&harborconfigurationv1alpha1.HarborConfiguration{}).
		Complete(r)
}

func (r *HarborConfigurationReconciler) registryReconciliation(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, registry modelv2.Registry, client *apiv2.RESTClient) (ctrl.Result, error) {
	srcRegistry, err := client.GetRegistryByName(ctx, harborConfiguration.Spec.Replication.RegistryName)
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
	srcRegistry, err := client.GetRegistryByName(ctx, harborConfiguration.Spec.Replication.RegistryName)
	if err != nil {
		return ctrl.Result{}, err
	}

	project := &modelv2.ProjectReq{
		ProjectName:  harborConfiguration.Spec.ProjectReq.ProjectName,
		Public:       harborConfiguration.Spec.ProjectReq.IsPublic,
		StorageLimit: harborConfiguration.Spec.ProjectReq.StorageLimit,
		RegistryID:   &srcRegistry.ID,
	}

	err = client.NewProject(ctx, project)
	if errors.Is(err, &harborerrors.ErrProjectNameAlreadyExists{}) {
		harborProject, err := client.GetProject(ctx, project.ProjectName)
		if err != nil {
			return ctrl.Result{}, err
		}
		update := &modelv2.Project{
			Name:       harborConfiguration.Spec.ProjectReq.ProjectName,
			RegistryID: srcRegistry.ID,
			ProjectID:  harborProject.ProjectID,
		}
		// Note: Only positive values of storageLimit are supported through this method.
		// Use the 'UpdateStorageQuotaByProjectID' method when `project.StorageLimit`is `-1`
		unlimitedStorage := int64(-1)
		if project.StorageLimit != &unlimitedStorage {
			err = client.UpdateProject(ctx, update, project.StorageLimit)
			if err != nil {
				return ctrl.Result{}, err
			}
		} else {
			err = client.UpdateStorageQuotaByProjectID(ctx, int64(update.ProjectID), unlimitedStorage)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *HarborConfigurationReconciler) replicationRuleReconciliation(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, registry modelv2.Registry, client *apiv2.RESTClient) (ctrl.Result, error) {
	srcRegistry, err := client.GetRegistryByName(ctx, harborConfiguration.Spec.Replication.RegistryName)
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
	return ctrl.Result{}, err
}

func (r *HarborConfigurationReconciler) reconcileAll(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, client *apiv2.RESTClient) (ctrl.Result, error) {
	registry := &modelv2.Registry{
		Name:        harborConfiguration.Spec.Registry.Name,
		Type:        harborConfiguration.Spec.Registry.Type,
		URL:         harborConfiguration.Spec.Registry.TargetRegistryUrl,
		Description: harborConfiguration.Spec.Registry.Description,
		Credential:  (*modelv2.RegistryCredential)(harborConfiguration.Spec.Registry.Credential),
	}

	_, err := r.registryReconciliation(ctx, harborConfiguration, *registry, client)
	if err != nil {
		return ctrl.Result{}, err
	}

	_, err = r.projectReconciliation(ctx, harborConfiguration, *registry, client)
	if err != nil {
		return ctrl.Result{}, err
	}

	_, err = r.replicationRuleReconciliation(ctx, harborConfiguration, *registry, client)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
}

func getHarborSecret(ctx context.Context, clientSet *kubernetes.Clientset, harborcluster *harborOperator.HarborCluster) (string, error) {
	passwordSecret, err := clientSet.CoreV1().Secrets(harborcluster.Namespace).Get(ctx, harborcluster.Spec.HarborAdminPasswordRef, v1.GetOptions{})
	if err != nil {
		return "", err
	}
	return string(passwordSecret.Data["secret"]), nil
}

func getHarborURL(harborcluster *harborOperator.HarborCluster) string {
	url := fmt.Sprintf("http://%s-harbor-harbor-core.%s/api/v2.0", harborcluster.Name, harborcluster.Namespace)
	return url
}

func getTypedKubeConfig() (*kubernetes.Clientset, error) {
	var config *rest.Config

	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = getkubeConfig()
		if err != nil {
			return nil, err
		}
	}

	kubeConfig, err := kubernetes.NewForConfig(config)

	return kubeConfig, err
}

func getDynamicKubeConfig() (dynamic.Interface, error) {
	var config *rest.Config

	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = getkubeConfig()
		if err != nil {
			return nil, err
		}
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Printf("error creating dynamic client: %v\n", err)
		os.Exit(1)
	}

	return dynamicClient, err
}

func getkubeConfig() (*rest.Config, error) {
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

	return kubeConfig, err
}

func getConcreteHarborType(ctx context.Context, crdClient dynamic.ResourceInterface, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, harborTarget harborOperator.HarborCluster) (harborOperator.HarborCluster, error) {
	harborUnstructured, err := crdClient.Get(ctx, harborConfiguration.Spec.HarborTarget.Name, v1.GetOptions{
		TypeMeta: v1.TypeMeta{
			Kind:       "HarborCluster",
			APIVersion: "v1alpha3",
		},
	})
	if err != nil {
		return harborTarget, err
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(harborUnstructured.UnstructuredContent(), &harborTarget)
	if err != nil {
		return harborTarget, err
	}
	return harborTarget, err
}

func deleteAll(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, client *apiv2.RESTClient) (ctrl.Result, error) {
	err := client.DeleteRegistryByID(ctx, harborConfiguration.Status.RegistryId)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = client.DeleteProject(ctx, harborConfiguration.Status.ProjectId)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = client.DeleteReplicationPolicyByID(ctx, harborConfiguration.Status.ReplicationId)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func triggerReplication(ctx context.Context, harborConfiguration harborconfigurationv1alpha1.HarborConfiguration, client *apiv2.RESTClient) (ctrl.Result, error) {
	replicationFound, err := client.GetReplicationPolicyByName(ctx, harborConfiguration.Spec.Replication.Name)
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
