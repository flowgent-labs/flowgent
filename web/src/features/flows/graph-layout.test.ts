import { describe, expect, it } from 'vitest'
import { graphLayers } from './graph-layout'

describe('graphLayers', () => {
  it('keeps feedback graphs finite while ranking the forward path', () => {
    const levels = graphLayers(
      ['start', 'fix', 'review', 'done'],
      [
        { from: 'start', to: 'fix' },
        { from: 'fix', to: 'review' },
        { from: 'review', to: 'done' },
        { from: 'done', to: 'fix' },
      ],
    )
    expect([...levels.values()]).toEqual([0, 1, 2, 3])
  })
})
