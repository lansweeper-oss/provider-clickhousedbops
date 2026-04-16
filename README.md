# Provider clickhousedbops

`provider-clickhousedbops` is a [Crossplane](https://crossplane.io/) provider
clickhousedbops that is built using [Upjet](https://github.com/crossplane/upjet) code
generation tools and exposes XRM-conformant managed resources for the Template
API.

## Getting Started

### Prerequisites

- A Kubernetes cluster with [Crossplane](https://crossplane.io/) installed
- A running ClickHouse instance

### Authentication

The provider connects to ClickHouse using credentials stored in a Kubernetes Secret.
The secret contains a JSON object with the connection details.

#### 1. Create a Secret with ClickHouse credentials

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: clickhousedbops-creds
  namespace: crossplane-system
type: Opaque
stringData:
  credentials: |
    {
      "host": "clickhouse.example.com",
      "port": 9000,
      "protocol": "native",
      "auth_config": {
        "strategy": "password",
        "username": "default",
        "password": "changeme"
      }
    }
```

or

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: clickhousedbops-creds
  namespace: crossplane-system
type: Opaque
stringData:
  credentials: |
    {
      "host": "clickhouse.example.com",
      "port": "8443",
      "protocol": "https",
      "auth_config": {
        "strategy": "basicauth",
        "username": "default",
        "password": "changeme"
      }
    }
```

#### 2. Create a ProviderConfig referencing the Secret

**Namespaced** (secret must be in the same namespace as the managed resources):

```yaml
apiVersion: clickhousedbops.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
  namespace: crossplane-system
spec:
  credentials:
    source: Secret
    secretRef:
      name: clickhousedbops-creds
      key: credentials
```

**Cluster-scoped** (for cluster-wide access):

```yaml
apiVersion: clickhousedbops.m.crossplane.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: clickhousedbops-creds
      namespace: crossplane-system
      key: credentials
```

The `credentials.source` field supports: `Secret`, `InjectedIdentity`, `Environment`, `Filesystem`, and `None`.

See the full examples in [`examples/namespaced/providerconfig/`](examples/namespaced/providerconfig/) and [`examples/cluster/providerconfig/`](examples/cluster/providerconfig/).

## Importing existing resources

See [docs/import.md](docs/import.md) for a full reference of the identity parameters needed
to import each resource type into Crossplane.

## Configuring User Passwords

For detailed information on configuring passwords for User resources, including three mutually exclusive methods
(auto-generate, plaintext secret reference, and hash secret reference), see [docs/password-configuration.md](docs/password-configuration.md).

## Developing

Run code-generation pipeline:

```console
go run cmd/generator/main.go "$PWD"
```

Run against a Kubernetes cluster (out of cluster):

```console
make run
```

or (deploying in-cluster):

```console
make local-deploy
```

Run e2e tests (locally in a KinD cluster):

```console
# UPTEST_SKIP_DELETE=true
make e2e
```

Build, push, and install:

```console
make all
```

Build binary:

```console
make build
```

## Report a Bug

For filing bugs, suggesting improvements, or requesting new features, please
open an [issue](https://github.com/lansweeper-oss/provider-clickhousedbops/issues).

## Licensing

`provider-clickhousedbops` is under the Apache 2.0 [license](LICENSE).
