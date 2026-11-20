import type { Repositories } from '../domain/repositories'
import { ApiClient } from './client'
import { HttpRuntimeConfigRepository } from './http-runtime-config-repository'
import {
  HttpAgentRepository,
  HttpAnalyticsRepository,
  HttpAgentGraphRepository,
  HttpKnowledgeRepository,
  HttpLlmRepository,
  HttpMcpRepository,
  HttpNotificationRepository,
  HttpGraphRunRepository,
  HttpSkillRepository,
  HttpRuntimeSkillRepository,
  HttpTraceRepository,
} from './http-repositories'

export function createRepositories(): Repositories {
  const api = new ApiClient()
  return {
    graphs: new HttpAgentGraphRepository(api),
    graphRuns: new HttpGraphRunRepository(api),
    traces: new HttpTraceRepository(api),
    analytics: new HttpAnalyticsRepository(api),
    knowledge: new HttpKnowledgeRepository(api),
    agents: new HttpAgentRepository(api),
    mcps: new HttpMcpRepository(api),
    llms: new HttpLlmRepository(api),
    notifications: new HttpNotificationRepository(api),
    runtimeSkills: new HttpRuntimeSkillRepository(api),
    skills: new HttpSkillRepository(api),
    runtimeConfig: new HttpRuntimeConfigRepository(api),
  }
}
