import type { Repositories } from '../domain/repositories'
import { ApiClient } from './client'
import { HttpRuntimeConfigRepository } from './http-runtime-config-repository'
import {
  HttpAgentRepository,
  HttpAnalyticsRepository,
  HttpFlowRepository,
  HttpKnowledgeRepository,
  HttpLlmRepository,
  HttpMcpRepository,
  HttpNotificationRepository,
  HttpRunRepository,
  HttpRuntimeSkillRepository,
  HttpTraceRepository,
} from './http-repositories'

export function createRepositories(): Repositories {
  const api = new ApiClient()
  return {
    flows: new HttpFlowRepository(api),
    runs: new HttpRunRepository(api),
    traces: new HttpTraceRepository(api),
    analytics: new HttpAnalyticsRepository(api),
    knowledge: new HttpKnowledgeRepository(api),
    agents: new HttpAgentRepository(api),
    mcps: new HttpMcpRepository(api),
    llms: new HttpLlmRepository(api),
    notifications: new HttpNotificationRepository(api),
    runtimeSkills: new HttpRuntimeSkillRepository(api),
    runtimeConfig: new HttpRuntimeConfigRepository(api),
  }
}
