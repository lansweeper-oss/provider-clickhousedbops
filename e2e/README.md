# E2E Tests

End-to-end tests run via [chainsaw](https://kyverno.github.io/chainsaw/) against
a local ClickHouse OSS instance deployed in-cluster.

## Structure

```
e2e/
  setup/          # ClickHouse deployment + service manifests
  manifests/      # Shared resource manifests (apply-and-assert via uptest.mk in CI)
  tests/
    cluster/      # Tests for cluster-scoped resources (clickhousedbops.crossplane.io)
    namespaced/   # Tests for namespaced resources (clickhousedbops.m.crossplane.io)
    common/       # Tests that apply to both scopes
```

## Running

```bash
make e2e  # runs chainsaw tests
```

## ClickHouse Cloud-only resources

Some resources require ClickHouse Cloud and cannot be tested in the e2e suite,
which uses the open-source ClickHouse Docker image. The OSS server rejects
the DDL for these resources.

| Resource | API Group | Reason |
|----------|-----------|--------|
| Masking Policy | `clickhousedbops.crossplane.io` / `clickhousedbops.m.crossplane.io` | Masking policies are only available on ClickHouse Cloud (v25.12+). OSS ClickHouse rejects `CREATE MASKING POLICY`. |

All other resources (Database, User, Role, GrantRole, GrantPrivilege,
SettingProfile, SettingProfileAssociation, Setting, Row Policy) work on
OSS ClickHouse and are testable in the e2e suite.
