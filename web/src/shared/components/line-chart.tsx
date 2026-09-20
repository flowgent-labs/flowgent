import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

export interface LineChartSeries {
  color: string
  name: string
  values: number[]
}

interface Point {
  x: number
  y: number
}

const width = 960
const height = 280
const padding = { top: 18, right: 18, bottom: 34, left: 44 }

export function LineChartView({
  labels,
  series,
  onLegendSelect,
}: {
  labels: string[]
  series: LineChartSeries[]
  onLegendSelect?: (name: string) => void
}) {
  const { t } = useTranslation()
  const [activeIndex, setActiveIndex] = useState<number | null>(null)
  const chart = useMemo(() => {
    const maximum = Math.max(4, ...series.flatMap((item) => item.values))
    const plotWidth = width - padding.left - padding.right
    const plotHeight = height - padding.top - padding.bottom
    const denominator = Math.max(labels.length - 1, 1)
    const xAt = (index: number) => padding.left + (index / denominator) * plotWidth
    const yAt = (value: number) => padding.top + plotHeight - (value / maximum) * plotHeight
    return {
      maximum,
      xAt,
      yAt,
      lines: series.map((item) => ({
        ...item,
        points: item.values.map((value, index) => ({ x: xAt(index), y: yAt(value) })),
      })),
    }
  }, [labels.length, series])

  const hoveredX = activeIndex === null ? 0 : chart.xAt(activeIndex)

  return (
    <div className="line-chart">
      <div className="line-chart__legend" aria-label={t('dashboard.chartLegend')}>
        {series.map((item) => (
          <button key={item.name} type="button" onClick={() => onLegendSelect?.(item.name)}>
            <span style={{ background: item.color }} />
            {item.name}
          </button>
        ))}
      </div>
      <div className="line-chart__plot">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="none"
          role="img"
          aria-label={t('dashboard.chartAria')}
          onPointerLeave={() => setActiveIndex(null)}
          onPointerMove={(event) => {
            if (!labels.length) return
            const bounds = event.currentTarget.getBoundingClientRect()
            const relativeX = ((event.clientX - bounds.left) / bounds.width) * width
            const ratio = (relativeX - padding.left) / (width - padding.left - padding.right)
            setActiveIndex(Math.round(Math.max(0, Math.min(1, ratio)) * (labels.length - 1)))
          }}
        >
          <defs>
            {series.map((item, index) => (
              <linearGradient key={item.name} id={`line-fill-${index}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={item.color} stopOpacity="0.22" />
                <stop offset="100%" stopColor={item.color} stopOpacity="0" />
              </linearGradient>
            ))}
          </defs>
          {[0, 1, 2, 3, 4].map((tick) => {
            const y = padding.top + (tick / 4) * (height - padding.top - padding.bottom)
            const value = Math.round(chart.maximum * (1 - tick / 4))
            return (
              <g key={tick}>
                <line
                  className="line-chart__grid"
                  x1={padding.left}
                  x2={width - padding.right}
                  y1={y}
                  y2={y}
                />
                <text
                  className="line-chart__label"
                  x={padding.left - 10}
                  y={y + 4}
                  textAnchor="end"
                >
                  {value}
                </text>
              </g>
            )
          })}
          {labels.map((label, index) => {
            const step = Math.max(1, Math.ceil(labels.length / 6))
            if (index % step !== 0 && index !== labels.length - 1) return null
            return (
              <text
                className="line-chart__label"
                key={`${label}-${index}`}
                x={chart.xAt(index)}
                y={height - 8}
                textAnchor="middle"
              >
                {label}
              </text>
            )
          })}
          {chart.lines.map((line, index) => {
            const path = toPath(line.points)
            const baseline = height - padding.bottom
            const firstPoint = line.points[0]
            const lastPoint = line.points.at(-1)
            const area =
              firstPoint && lastPoint
                ? `${path} L ${lastPoint.x} ${baseline} L ${firstPoint.x} ${baseline} Z`
                : ''
            return (
              <g key={line.name}>
                <path d={area} fill={`url(#line-fill-${index})`} />
                <path className="line-chart__line" d={path} stroke={line.color} />
              </g>
            )
          })}
          {activeIndex !== null && (
            <>
              <line
                className="line-chart__cursor"
                x1={hoveredX}
                x2={hoveredX}
                y1={padding.top}
                y2={height - padding.bottom}
              />
              {chart.lines.map((line) => (
                <circle
                  key={line.name}
                  cx={line.points[activeIndex]?.x}
                  cy={line.points[activeIndex]?.y}
                  r="4"
                  fill={line.color}
                  stroke="var(--surface)"
                  strokeWidth="2"
                  vectorEffect="non-scaling-stroke"
                />
              ))}
            </>
          )}
        </svg>
        {activeIndex !== null && (
          <div className="line-chart__tooltip" style={{ left: `${(hoveredX / width) * 100}%` }}>
            <strong>{labels[activeIndex]}</strong>
            {series.map((item) => (
              <span key={item.name}>
                <i style={{ background: item.color }} />
                {item.name}
                <b>{item.values[activeIndex] ?? 0}</b>
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function toPath(points: Point[]) {
  return points.map((point, index) => `${index ? 'L' : 'M'} ${point.x} ${point.y}`).join(' ')
}
