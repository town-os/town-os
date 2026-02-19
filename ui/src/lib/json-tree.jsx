import { useState } from 'react'
import { ChevronRight, ChevronDown } from 'lucide-react'

/**
 * Recursively renders a JSON value as a collapsible tree.
 * @param {{ label: string, value: any }} props
 */
function JsonNode({ label, value }) {
  const [expanded, setExpanded] = useState(true)

  if (value === null) {
    return (
      <div className="flex items-center gap-1 py-0.5">
        <span className="text-muted-foreground">{label}:</span>
        <span className="text-orange-500 italic">null</span>
      </div>
    )
  }

  if (typeof value === 'object' && !Array.isArray(value)) {
    const keys = Object.keys(value)
    return (
      <div>
        <div
          className="flex items-center gap-0.5 py-0.5 cursor-pointer select-none hover:bg-muted/50 rounded"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded
            ? <ChevronDown className="h-3 w-3 text-muted-foreground shrink-0" />
            : <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />}
          <span className="text-muted-foreground">{label}</span>
          {!expanded && (
            <span className="text-muted-foreground ml-1">
              {`{${keys.length}}`}
            </span>
          )}
        </div>
        {expanded && (
          <div className="ml-4 border-l border-border pl-2">
            {keys.map((k) => (
              <JsonNode key={k} label={k} value={value[k]} />
            ))}
          </div>
        )}
      </div>
    )
  }

  if (Array.isArray(value)) {
    return (
      <div>
        <div
          className="flex items-center gap-0.5 py-0.5 cursor-pointer select-none hover:bg-muted/50 rounded"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded
            ? <ChevronDown className="h-3 w-3 text-muted-foreground shrink-0" />
            : <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />}
          <span className="text-muted-foreground">{label}</span>
          {!expanded && (
            <span className="text-muted-foreground ml-1">
              {`[${value.length}]`}
            </span>
          )}
        </div>
        {expanded && (
          <div className="ml-4 border-l border-border pl-2">
            {value.map((item, i) => (
              <JsonNode key={i} label={String(i)} value={item} />
            ))}
          </div>
        )}
      </div>
    )
  }

  if (typeof value === 'boolean') {
    return (
      <div className="flex items-center gap-1 py-0.5">
        <span className="text-muted-foreground">{label}:</span>
        <span className="text-blue-500">{String(value)}</span>
      </div>
    )
  }

  if (typeof value === 'number') {
    return (
      <div className="flex items-center gap-1 py-0.5">
        <span className="text-muted-foreground">{label}:</span>
        <span className="text-green-600">{value}</span>
      </div>
    )
  }

  return (
    <div className="flex items-center gap-1 py-0.5">
      <span className="text-muted-foreground">{label}:</span>
      <span className="text-foreground">&quot;{String(value)}&quot;</span>
    </div>
  )
}

/**
 * Renders a JSON string as a collapsible tree view.
 * @param {{ data: string }} props - JSON string
 */
export function JsonTree({ data }) {
  if (!data) return <span className="text-muted-foreground">No details</span>

  let parsed
  try {
    parsed = JSON.parse(data)
  } catch {
    return <pre className="text-xs whitespace-pre-wrap">{data}</pre>
  }

  if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
    return (
      <div className="font-mono text-sm">
        {Object.keys(parsed).map((k) => (
          <JsonNode key={k} label={k} value={parsed[k]} />
        ))}
      </div>
    )
  }

  return <JsonNode label="root" value={parsed} />
}
