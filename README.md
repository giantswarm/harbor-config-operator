# harbor-config-operator

Currently a PoC for an operator that will administer harbor registeries, projects and replication rules with execution.

## Development

### Build

Makefile targets exists which build the controller and generate the kubernetes manifests. To build and generate run the following:

```sh
make build
```

### Deploy

To run the controller locally against a local kind cluster, it's recommnded to port-forward to the `harbor-core` pod and set the HARBOR_CORE_URL. For instance:

- ```sh
  kubectl -n harbor-cluster port-forward -l goharbor.io/operator-controller=core 8080:80
  ```

- ```sh
  export HARBOR_CORE_URL="http://127.0.0.1:8080/api/v2.0"
  ```

- ```sh
  make install
  ```

- ```sh
  make run
  ```
### Test

To execute the controller tests:
```sh
make test
```
