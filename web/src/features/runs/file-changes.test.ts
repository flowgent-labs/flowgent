import { describe, expect, it } from 'vitest'
import type { TaskRun } from '../../core/domain/types'
import { buildLineDiff, diffStats, extractTaskFileChanges } from './file-changes'

function task(input: Record<string, unknown>, output: Record<string, unknown>): TaskRun {
  return {
    id: 'task-1',
    status: 'SUCCESS',
    created_at: '2026-10-12T00:00:00Z',
    updated_at: '2026-10-12T00:00:01Z',
    run_id: 'run-1',
    node_key: 'generate-fixes',
    attempt: 1,
    input,
    output,
    error: '',
    max_retries: 2,
    execution_id: 'exec-1',
    sequence: 1,
    fencing_token: 1,
  }
}

describe('task file change projection', () => {
  it('pairs generated full-file output with its original source input', () => {
    expect(
      extractTaskFileChanges(
        task(
          {
            source_files: [{ path: 'src/auth.ts', content: 'const safe = false\n' }],
          },
          {
            files: [{ path: 'src/auth.ts', content: 'const safe = true\n' }],
            patches: [
              {
                file: 'src/auth.ts',
                rule: 'typescript:S2068',
                description: 'Remove the insecure default.',
              },
            ],
          },
        ),
      ),
    ).toEqual([
      {
        path: 'src/auth.ts',
        before: 'const safe = false\n',
        after: 'const safe = true\n',
        kind: 'modified',
        rule: 'typescript:S2068',
        description: 'Remove the insecure default.',
      },
    ])
  })

  it('understands a sandbox JSON payload wrapped in stdout', () => {
    expect(
      extractTaskFileChanges(
        task(
          {},
          {
            stdout: JSON.stringify({
              files: [{ path: 'src/new.ts', content: 'export const secured = true\n' }],
            }),
          },
        ),
      )[0],
    ).toMatchObject({ path: 'src/new.ts', kind: 'added' })
  })

  it('produces line-addressable additions and deletions', () => {
    const lines = buildLineDiff('one\ntwo\nthree', 'one\nsecured\nthree')
    expect(lines).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ kind: 'deletion', text: 'two', oldLine: 2 }),
        expect.objectContaining({ kind: 'addition', text: 'secured', newLine: 2 }),
      ]),
    )
    expect(diffStats(lines)).toEqual({ additions: 1, deletions: 1 })
  })
})
