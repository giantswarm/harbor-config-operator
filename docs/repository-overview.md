# Harbor configuration operator overview

## Purpose

The purpose of this operator is to reconcile registries and projects for the [harbor-operator-app](https://github.com/giantswarm/harbor-operator-app). It can also cause replications to trigger, however, this is not neccessary as the preferred method is to create a proxy cache project to pull through. This project was built as a proof of concept and as of such does not include tests. An alternative to this project can be found [here](https://github.com/mittwald/harbor-operator) which provides more functionality than what has currently been implemented. This repository has not been tested yet with the harbor-operator-app, but may be worth exploring into. It comes from the same github user that hosts the api sdk used in this project.

## How does this project work?

This project was built using the [kubebuilder](https://book.kubebuilder.io/) framework for kuberentes operators. We create structs for the api which can be found at: (/harbor-config-operator/api/v1alpha1/harborconfiguration_types.go), which can then be used as fields in custom resource definitions. These custom reources are then passed into the controller found at: (/harbor-config-operator/controllers/harborconfiguration_controller.go), where a reconcile loop is used to match expected state with actual state.

*Example Custom Resource*

```
apiVersion: administration.harbor.configuration/v1alpha1
kind: HarborConfiguration
metadata:
  name: setup-giantswarm-project
  namespace: harbor-cluster
spec:
  harborTarget:
    name: harbor-cluster
    namespace: harbor-cluster
    harborUsername: admin
  registry:
    name: docker
    provider: docker-hub
    endpointUrl: https://hub.docker.com
    description: pull from dockerhub
  projectReq:
    projectName: giantswarm
    storageQuota: -1
    public: true
    proxyCacheRegistryName: docker
```

### Explanation of fields

**harborTarget**

harborTarget is used to find the `HarborCluster` kind deployed on your kubernetes cluster, so that the operator has a target instance to reconcile agianst. The `HarborCluster` kind is configured in the [fullstack_manifest.yaml](https://github.com/giantswarm/harbor-operator-app/blob/master/config/samples/harbor-cluster-configuration.yaml), which can also be found under the extras folder in [management-cluster-fleet](https://github.com/giantswarm/management-clusters-fleet/extras) (if you can't find it check the add-harbor-operator branch). The name will depend in what is configured for your kind, here we have called it `harbor-cluster`, thus matching the name to the kind. The target namespace should always be `harbor-cluster`, where the components of harbor (such as the harbor-core deployment) can be found. By default the username for a harbor instance is always `admin`.

**registry**

registry is used to create registries in the harbor instance via internal api calls. Here we provide the name we'd like our registry to have and the correct provider for that name. We then provide the correct endpoint and an optional description. If you are struggling to correctly populate these fields, it can help to use the harbor UI to create a registry first, whilst viewing the request with the `inspector` panel in your browser.

**projectReq**

projectReq is used to create projects in your harbor instance. A proxy cache project will always require a registry to pull from. projectname is what your project will be called, storageQuota determines the memory allocation of your project (with -1 being unlimited). Public determines whether or not authentication against your harbor instance is needed to access the project and proxyCacheRegistryName is an optional field, which when given, will mean your project is created as a proxy cache.

**replication**

replication should not be neccesary for this implementation as we are intending to use harbor strictly as a proxy cache. An example of a replication cr can be found under (/harbor-config-operator/config/samples/quay-replication-cr.yaml).

## Testing

For information on testing checkout the README.md, which explains how you would want to test the operator on a local environment.

## Deployment

Primarily, we expect this operator to be deployed in the [management-clusters-fleet](https://github.com/giantswarm/management-clusters-fleet) via flux. Flux will reconcile the helm templates when configured to the correct management cluster.

Deployment can also be done locally with helm by running:

```
helm install harbor-config-operator --namespace harbor-cluster -f ./values.yaml .
```

From the `/harbor-config-operator/helm/harbor-config-operator` directory.

On management clusters you can also deploy using the `app-reconciler` through the following yaml file:

```
apiVersion: application.giantswarm.io/v1alpha1
kind: App
metadata:
  labels:
    app-operator.giantswarm.io/version: 0.0.0
    app.kubernetes.io/name: harbor-config-operator
  name: harbor-config-operator
  namespace: giantswarm   
spec:
  catalog: control-plane-test-catalog
  kubeConfig:
    inCluster: true
  name: harbor-config-operator
  namespace: harbor-cluster
  version: 0.0.0-dcd54e74532cccebb6b107b25cbb540faa441428
```

Make sure that the version and the app catalog are correct by checking circle ci before you deploy.

## How does this interact with harbor?

As previously mentioned this project interacts with a specific harbor instance via the HarborCluster kind. Because it does this internally, it is important that both applications are on the same network. It does this by making api calls through the `harbor-core` pod which is usually in the `harbor-cluster` namespace.

## Debugging

In the case you need to debug follow the instructions on the README.md to connect your local network to the cluster hosting harbor. Now that they can communicate you should use the debug feature in your IDE to step through the code and find where the error is happening. If the error is from things like HTTP requests it is likely a networking issue that lies outside of this project in the actual cluster itself.

## Notes for the future

- In the controller the apigroup of `harbor` which has been set for the operator to match against is `v1alpha3`. In the future there is a chance that the harbor instance may change api groups to something like `v1beta1` so if the operator isn't working, verify that harbor is using the expected api version.

- There is currently no planned maintenance for this repository so things might go out of date or change. Specifically, the CVEs in the nancy.ignore file are set to expire at the end of 2023 which will cause the circle ci pipeline to fail.

- This project was originally built as a PoC so if you are looking to deploy it into an enterprise setting I highly recommend either writing tests or looking into using [this](https://github.com/mittwald/harbor-operator) aforementioned project, which has had a longer development cycle.
