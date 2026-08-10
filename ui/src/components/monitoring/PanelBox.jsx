/**
 * The bordered frame one monitoring chart sits in. Shared by every
 * dashboard tab so a panel cannot pick up different padding or border
 * treatment depending on which grid it happens to be rendered in.
 *
 * className is where the caller supplies sizing: the system tab's grid
 * gives its cells a height, while the DNS tab pins a per-panel height and
 * scrolls, because eight panels do not fit a viewport at a readable size.
 */
export default function PanelBox({ className = '', children }) {
  return (
    <div className={`min-h-0 min-w-0 overflow-hidden rounded-md border p-1.5 ${className}`}>
      {children}
    </div>
  )
}
