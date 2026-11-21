import type { RuntimeConfigRepository } from '../domain/repositories'
import type { RuntimeConfigView, RuntimeSecretUpdate } from '../domain/types'
import { ApiClient, namespacePath } from './client'

export class HttpRuntimeConfigRepository implements RuntimeConfigRepository {
  constructor(private readonly api: ApiClient) {}

  getNamespace(namespace: string, signal?: AbortSignal) {
    return this.api.request<RuntimeConfigView>(namespacePath(namespace, 'runtime-config'), {
      signal,
    })
  }

  updateNamespaceEnvironment(namespace: string, environment: Record<string, string>) {
    return this.api.request<RuntimeConfigView>(
      namespacePath(namespace, 'runtime-config/environment'),
      {
        method: 'PUT',
        body: JSON.stringify({ environment }),
      },
    )
  }

  updateNamespaceSecrets(namespace: string, update: RuntimeSecretUpdate) {
    return this.api.request<RuntimeConfigView>(namespacePath(namespace, 'runtime-config/secrets'), {
      method: 'PUT',
      body: JSON.stringify(update),
    })
  }

  getAgentGraph(namespace: string, graphId: string, signal?: AbortSignal) {
    return this.api.request<RuntimeConfigView>(
      namespacePath(namespace, `graphs/${encodeURIComponent(graphId)}/runtime-config`),
      { signal },
    )
  }

  updateAgentGraphEnvironment(
    namespace: string,
    graphId: string,
    environment: Record<string, string>,
  ) {
    return this.api.request<RuntimeConfigView>(
      namespacePath(namespace, `graphs/${encodeURIComponent(graphId)}/runtime-config/environment`),
      { method: 'PUT', body: JSON.stringify({ environment }) },
    )
  }

  updateAgentGraphSecrets(namespace: string, graphId: string, update: RuntimeSecretUpdate) {
    return this.api.request<RuntimeConfigView>(
      namespacePath(namespace, `graphs/${encodeURIComponent(graphId)}/runtime-config/secrets`),
      { method: 'PUT', body: JSON.stringify(update) },
    )
  }
}
