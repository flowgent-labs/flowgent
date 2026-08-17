# Flowgent Documentation

[中文索引](index_ZH.md)

This directory separates current design, applications, operational guidance,
development plans, and historical records. Follow the narrowest document
that owns the subject.

## Current Design

| Layer | Document | Scope |
|---|---|---|
| L1 Engine | [Engine overview](architecture/overview.md) | Cross-component calls, state and messaging contracts, deployment, and shared modules |
| L1 Engine | [API Server](architecture/engine/apiserver.md) | External gateway and sole durable-state client |
| L1 Engine | [Controller](architecture/engine/controller.md) | Active-run JobManager reconciliation |
| L1 Engine | [Resource Pools](architecture/engine/resource-pools.md) | Namespace-scoped worker capacity and SLA isolation |
| L1 Engine | [JobManager](architecture/engine/jobmanager.md) | DAG scheduling and runtime ownership |
| L1 Engine | [TaskManager](architecture/engine/taskmanager.md) | Slot execution and executor routing |
| L1 Engine | [Sandbox](architecture/engine/sandbox.md) | Isolated script execution |
| L1 Engine | [Notifier](architecture/engine/notifier.md) | Delivery and UI event boundary |
| L1 Engine | [Wallet](architecture/engine/wallet.md) | External key custody and digest-signing boundary |
| L2 AgentFlow | [AgentFlow application architecture](architecture/agent-flow.md) | Generic DAG model, capabilities, limits, and authoring practices |

## Applications

Each application owns its nodes, edges, integrations, limits, and verification
in `usecase/{use-case}/README.md`; executable resources remain beside it.
Shared DAG semantics belong to the
[AgentFlow application architecture](architecture/agent-flow.md).

| Application | Scope | Entries |
|---|---|---|
| Security Autonomy Fixer | SonarQube-to-GitHub remediation and post-change verification; 27-node/31-edge DAG | [Design](../usecase/security-autonomy-fixer/README.md) · [中文](../usecase/security-autonomy-fixer/README_ZH.md) · [Manifest](../usecase/security-autonomy-fixer/config/flows/security-autonomy-fixer.yaml) · [E2E](../usecase/security-autonomy-fixer/e2e/VERIFICATION.md) |
| AutoTest Generation | Confluence-driven test generation for Spring Boot, Flask, and React; 18-node/22-edge draft | [Design](../usecase/autotest-generator/README.md) · [中文](../usecase/autotest-generator/README_ZH.md) · [Manifest](../usecase/autotest-generator/config/flows/autotest-generation-v1.yaml) |

## Operations

| Document | Scope |
|---|---|
| [Dependency images](runbooks/build-dependency-images.md) | Build and operate local facilitator, EVM, and Solana images |

## Development and History

| Status | Location | Meaning |
|---|---|---|
| Active | [`development/`](development/) | Work that still carries current acceptance criteria |
| Historical | [`development/archive/`](development/archive/) | Early intent and dated implementation records; context only |

Current active development plan:

- [Security Fixer UI-only end-to-end completion](development/security-fixer-ui-e2e.md)

## Language Versions

- Current design, application, runbook, and active-development documents use files
  without `_ZH` as their canonical English source.
- Their Chinese review copies use the `_ZH.md` suffix beside the English source.
- Historical archives are frozen records and are exempt from active bilingual
  review pairing.
- Both versions MUST preserve the same design meaning, interfaces, constraints,
  and expected behavior.
- Diagrams and examples MAY be localized or compacted only when every depicted
  relationship, boundary, and limitation remains unchanged.
- A semantic change MUST update both language versions in the same change.

## Maintenance Rules

- Current behavior MUST be documented in the owning current-design document.
- Generic AgentFlow semantics MUST remain in `architecture/`; application-specific
  nodes, edges, integrations, and E2E evidence MUST remain in the owning
  `usecase/{use-case}/README.md`.
- Every real application MUST provide `README.md` and `README_ZH.md`. Its flows,
  agents, and MCP definitions MUST remain self-contained under
  `usecase/{use-case}/config/`; runtime credentials MUST remain outside manifests.
- Each design concept MUST have one canonical location; other documents MUST
  link to it instead of duplicating it.
- Design documents MUST distinguish implemented behavior, reserved design, and
  known limitations.
- Normative requirements MUST use explicit keywords such as `MUST`, `MUST NOT`,
  or `DO NOT`.
- Diagrams and examples MUST clarify contracts and data flow; they MUST NOT
  replace precise behavioral constraints.
- Historical documents MUST remain under `development/archive/` and MUST NOT be used
  as current implementation authority.
- Runtime manifests, source code, secrets, generated reports, and large assets
  MUST NOT be copied into `docs/`.
- Empty category trees and speculative placeholder documents MUST NOT be added.
- Links and headings MUST be validated before a documentation change is
  considered complete. Language pairs MUST also be validated for current and
  active documents.
