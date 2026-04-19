import { Fragment, useState } from 'react'
import { useI18n } from '@/i18n/I18nContext.jsx'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  ChevronRight,
  ChevronDown,
  Network,
  MoreHorizontal,
  Play,
  Square,
  RotateCcw,
  FileText,
} from 'lucide-react'

/**
 * Render a package unit row (parent or dependency). Root rows carry a
 * cascading action dropdown whose handlers fire tree-scoped start/stop/
 * restart; dependency rows carry per-unit actions so operators can still
 * restart a single dep without touching its siblings. Both kinds share
 * the same "view logs" items — this is the only user-facing delta from the
 * old flat /systemd/units table beyond the nesting.
 */
function countDescendants(node) {
  if (!node.children || node.children.length === 0) return 0
  let n = node.children.length
  for (const c of node.children) n += countDescendants(c)
  return n
}

function statusBadge(node, failedLabel) {
  const active = node.ActiveState
  const variant =
    active === 'active' ? 'default' : active === 'failed' ? 'destructive' : 'secondary'
  const label = node.nc_failed ? failedLabel : active
  return <Badge variant={variant}>{label}</Badge>
}

function TreeRow({
  node,
  depth,
  onCascadeAction,
  onUnitAction,
  onViewLogs,
  onViewGroupLogs,
  onViewNetworkLogs,
  expanded,
  onToggle,
  actionInProgress,
}) {
  const { t } = useI18n()
  const hasChildren = node.children && node.children.length > 0
  const isExpanded = expanded[node.package_identifier] === true
  const isRoot = depth === 0
  const descendantCount = countDescendants(node)
  const paddingLeft = 8 + depth * 20

  return (
    <>
      <TableRow
        className={hasChildren ? 'cursor-pointer hover:bg-muted/50' : undefined}
        onClick={hasChildren ? () => onToggle(node.package_identifier) : undefined}
        data-testid={`service-tree-row-${node.package_identifier}`}
      >
        <TableCell className="font-medium">
          <div className="flex items-center gap-1" style={{ paddingLeft }}>
            {hasChildren ? (
              isExpanded ? (
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
              ) : (
                <ChevronRight className="h-4 w-4 text-muted-foreground" />
              )
            ) : (
              <span className="inline-block w-4" />
            )}
            {!isRoot && (
              <Network className="h-3 w-3 text-muted-foreground" aria-hidden="true" />
            )}
            <span className="font-mono text-sm">
              {node.display_identifier || node.package_identifier}
            </span>
            {hasChildren && (
              <span className="text-xs text-muted-foreground ml-2">
                ({descendantCount === 1
                  ? t('system.tree_dep_count', { count: descendantCount })
                  : t('system.tree_dep_count_plural', { count: descendantCount })})
              </span>
            )}
          </div>
        </TableCell>
        <TableCell className="text-sm text-muted-foreground">
          {node.package_description || ''}
        </TableCell>
        <TableCell>
          {statusBadge(node, t('system.status_failed_nc'))}
        </TableCell>
        <TableCell className="text-right">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                aria-label={t('system.actions_label')}
                onClick={(e) => e.stopPropagation()}
              >
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem
                disabled={actionInProgress}
                onClick={(e) => {
                  e.stopPropagation()
                  if (isRoot) onCascadeAction(node, 'start')
                  else onUnitAction(node, 'start')
                }}
              >
                <Play className="h-3 w-3 mr-2" />
                {t('system.action_start')}
              </DropdownMenuItem>
              {node.Name !== 'town-os-systemcontroller.service' && (
                <DropdownMenuItem
                  disabled={actionInProgress}
                  onClick={(e) => {
                    e.stopPropagation()
                    if (isRoot) onCascadeAction(node, 'stop')
                    else onUnitAction(node, 'stop')
                  }}
                >
                  <Square className="h-3 w-3 mr-2" />
                  {t('system.action_stop')}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                disabled={actionInProgress}
                onClick={(e) => {
                  e.stopPropagation()
                  if (isRoot) onCascadeAction(node, 'restart')
                  else onUnitAction(node, 'restart')
                }}
              >
                <RotateCcw className="h-3 w-3 mr-2" />
                {t('system.action_restart')}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={(e) => {
                  e.stopPropagation()
                  onViewLogs(node)
                }}
              >
                <FileText className="h-3 w-3 mr-2" />
                {t('system.action_service_logs')}
              </DropdownMenuItem>
              {isRoot && node.children && node.children.length > 0 && onViewGroupLogs && (
                <DropdownMenuItem
                  onClick={(e) => {
                    e.stopPropagation()
                    onViewGroupLogs(node)
                  }}
                >
                  <FileText className="h-3 w-3 mr-2" />
                  {t('system.action_group_logs')}
                </DropdownMenuItem>
              )}
              {node.nc_active && (
                <DropdownMenuItem
                  onClick={(e) => {
                    e.stopPropagation()
                    onViewNetworkLogs(node)
                  }}
                >
                  <FileText className="h-3 w-3 mr-2" />
                  {t('system.action_network_logs')}
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </TableCell>
      </TableRow>
      {hasChildren && isExpanded && node.children.map((child) => (
        <Fragment key={child.package_identifier}>
          <TreeRow
            node={child}
            depth={depth + 1}
            onCascadeAction={onCascadeAction}
            onUnitAction={onUnitAction}
            onViewLogs={onViewLogs}
            onViewGroupLogs={onViewGroupLogs}
            onViewNetworkLogs={onViewNetworkLogs}
            expanded={expanded}
            onToggle={onToggle}
            actionInProgress={actionInProgress}
          />
        </Fragment>
      ))}
    </>
  )
}

export default function PackageServiceTree({
  roots,
  onCascadeAction,
  onUnitAction,
  onViewLogs,
  onViewGroupLogs,
  onViewNetworkLogs,
  actionInProgress,
}) {
  const { t } = useI18n()
  const [expanded, setExpanded] = useState({})

  function toggle(key) {
    setExpanded((prev) => ({ ...prev, [key]: prev[key] !== true }))
  }

  if (!roots || roots.length === 0) {
    return (
      <div className="rounded-md border p-6 text-center text-sm text-muted-foreground">
        {t('system.tree_no_packages')}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead style={{ width: '35%' }}>{t('system.col_package')}</TableHead>
              <TableHead style={{ width: '35%' }}>{t('system.col_description')}</TableHead>
              <TableHead style={{ width: '15%' }}>{t('system.col_status')}</TableHead>
              <TableHead className="text-right" style={{ width: '15%' }}>{t('system.col_actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {roots.map((node) => (
              <TreeRow
                key={node.package_identifier}
                node={node}
                depth={0}
                onCascadeAction={onCascadeAction}
                onUnitAction={onUnitAction}
                onViewLogs={onViewLogs}
                onViewGroupLogs={onViewGroupLogs}
                onViewNetworkLogs={onViewNetworkLogs}
                expanded={expanded}
                onToggle={toggle}
                actionInProgress={actionInProgress}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
