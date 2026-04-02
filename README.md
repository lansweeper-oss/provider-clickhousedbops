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

When creating a User resource, you must configure the password using one of three mutually exclusive methods:

### 1. Auto-generate Password

Let the provider generate a secure random password and store it in a Kubernetes Secret:

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    autoGeneratePassword: true
  writeConnectionSecretToRef:
    name: myuser-credentials
    namespace: default
  providerConfigRef:
    name: default
```

The generated secret will contain:
- `password`: plaintext password
- `hash`: SHA256 hash of the password

### 2. Reference Existing Plaintext Secret

Point to a Kubernetes Secret containing a plaintext password.
The controller will automatically compute the SHA256 hash and store it in the same secret:

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    passwordSecretRef:
      name: my-password-secret
      key: password
      # namespace: default <-- optional, defaults to resource namespace
  providerConfigRef:
    name: default
```

The secret must exist and contain the plaintext password at the specified key.
The controller will add a `hash` key with the computed SHA256 hash.

### 3. Reference Existing Hash Secret

Provide a reference to a secret that already contains the SHA256 hash:

```yaml
apiVersion: clickhousedbops.crossplane.io/v1alpha1
kind: User
metadata:
  name: myuser
  namespace: default
spec:
  forProvider:
    name: myuser
    passwordSha256HashSecretRef:
      name: my-password-hash-secret
      key: hash
      namespace: default
  providerConfigRef:
    name: default
```

The secret must exist and contain the SHA256 hash at the specified key.

### Mutual Exclusivity

Only one of `autoGeneratePassword`, `passwordSecretRef`, or `passwordSha256HashSecretRef` may be set.
The provider will fail immediately if multiple methods are configured.

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

`provider-mongodbatlas` is under the Apache 2.0 [license](LICENSE).
