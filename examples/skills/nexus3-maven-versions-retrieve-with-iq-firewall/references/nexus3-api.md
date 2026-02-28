# Nexus3 REST API Reference

Endpoints used by this skill.

## Component Search

```
GET /service/rest/v1/search
  ?group={groupId}
  &name={artifactId}
  &sort=version
  &direction=desc
```

Returns paginated `items[]` with `version`, `id`, `group`, `name`, `format`.

Auth: HTTP Basic (`NEXUS3_USER` / `NEXUS3_PASSWORD`).

## Firewall Status (SonatypeIQ license required)

```
GET /service/rest/v1/firewall/component
  ?group={groupId}
  &name={artifactId}
  &version={version}
```

Returns `{"firewallStatus": "allow"|"blocked"}` if IQ license is active.
Returns empty `{}` or 404 on OSS deployments. Treat missing status as `"unknown"`.

## gcloud Artifact Registry (alternative)

```bash
gcloud artifacts packages list \
  --repository={repo} \
  --location={region} \
  --package={artifactId} \
  --format="value(firewallStatus)"
```

Returns firewall status if the artifact is mirrored in Google Artifact Registry.
