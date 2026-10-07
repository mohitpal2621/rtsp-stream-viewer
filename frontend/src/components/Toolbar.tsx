import type { Columns } from './StreamWall'

const COLUMN_OPTIONS: { value: Columns; label: string }[] = [
  { value: 'auto', label: 'Auto' },
  { value: 1, label: '1' },
  { value: 2, label: '2' },
  { value: 3, label: '3' },
  { value: 4, label: '4' },
]

interface Props {
  count: number
  pausedCount: number
  columns: Columns
  onColumnsChange: (columns: Columns) => void
  onPauseAll: () => void
  onPlayAll: () => void
}

export function Toolbar({ count, pausedCount, columns, onColumnsChange, onPauseAll, onPlayAll }: Props) {
  const allPaused = count > 0 && pausedCount === count
  return (
    <div className="toolbar">
      <p className="toolbar__count">
        {count === 1 ? '1 stream' : `${count} streams`}
        {pausedCount > 0 && `, ${pausedCount} paused`}
      </p>

      <fieldset className="segmented">
        <legend className="segmented__legend">Columns</legend>
        {COLUMN_OPTIONS.map((option) => (
          <label key={option.value} className="segmented__option">
            <input
              type="radio"
              name="columns"
              value={option.value}
              checked={columns === option.value}
              onChange={() => onColumnsChange(option.value)}
            />
            <span>{option.label}</span>
          </label>
        ))}
      </fieldset>

      <button type="button" className="button" onClick={allPaused ? onPlayAll : onPauseAll} disabled={count === 0}>
        {allPaused ? 'Play all' : 'Pause all'}
      </button>
    </div>
  )
}
