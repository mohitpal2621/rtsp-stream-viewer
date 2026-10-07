// Small inline icons. They inherit the text colour and are hidden from
// screen readers; the buttons that hold them carry the label.

const common = {
  width: 18,
  height: 18,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
}

export function PlayIcon() {
  return (
    <svg {...common}>
      <path d="M7 4.5v15l12-7.5z" fill="currentColor" />
    </svg>
  )
}

export function PauseIcon() {
  return (
    <svg {...common}>
      <path d="M8 5v14M16 5v14" strokeWidth={3} />
    </svg>
  )
}

export function FullscreenIcon() {
  return (
    <svg {...common}>
      <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
    </svg>
  )
}

export function CloseIcon() {
  return (
    <svg {...common}>
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  )
}
