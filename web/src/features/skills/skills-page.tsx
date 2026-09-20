import { RuntimeSkills } from './runtime-skills'

// Runtime Skills are server-backed DAG resources. Portable local-only package
// drafts were intentionally removed: the console never emulates an API.
export function SkillsPage() {
  return <RuntimeSkills />
}
