import { RuntimeSkills } from './runtime-skills'
import { SkillDefinitions } from './skill-definitions'

// Runtime Skills are server-backed DAG resources. Portable local-only package
// drafts were intentionally removed: the console never emulates an API.
export function SkillsPage() {
  return (
    <div className="page-stack">
      <SkillDefinitions />
      <RuntimeSkills />
    </div>
  )
}
