#!/usr/bin/env bash
set -aeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

echo "Running setup.sh"

echo "Deploying local ClickHouse cluster..."
${KUBECTL} apply -f "${PROJECT_ROOT}/e2e/setup/clickhouse.yaml"

echo "Waiting for ClickHouse to be ready..."
${KUBECTL} -n clickhouse wait deployment/clickhouse \
  --for=condition=Available --timeout=5m

echo "Creating provider credentials secret pointing to local ClickHouse..."
${KUBECTL} -n crossplane-system create secret generic provider-secret \
  --from-literal=credentials='{"host":"clickhouse.clickhouse.svc.cluster.local","port":9000,"protocol":"native","auth_config":{"strategy":"password","username":"default","password":"e2epassword"}}' \
  --dry-run=client -o yaml | ${KUBECTL} apply -f -

echo "Waiting until provider is healthy..."
${KUBECTL} wait provider.pkg --all --for condition=Healthy --timeout 5m

echo "Waiting for all pods to come online..."
${KUBECTL} -n crossplane-system wait --for=condition=Available deployment --all --timeout=5m

echo "Creating a default provider config..."
cat <<EOF | ${KUBECTL} apply -f -
apiVersion: clickhousedbops.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: provider-secret
      namespace: crossplane-system
      key: credentials
EOF

echo "Creating a default cluster provider config (v2-style)..."
cat <<EOF | ${KUBECTL} apply -f -
apiVersion: clickhousedbops.m.crossplane.io/v1beta1
kind: ClusterProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: provider-secret
      namespace: crossplane-system
      key: credentials
EOF

${KUBECTL} wait provider.pkg --all --for condition=Healthy --timeout 5m
${KUBECTL} -n crossplane-system wait --for=condition=Available deployment --all --timeout=5m
