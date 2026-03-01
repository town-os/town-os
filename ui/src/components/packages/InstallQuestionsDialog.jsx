import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { X } from 'lucide-react'

export default function InstallQuestionsDialog({ dialog, onClose, onSubmit, onClearField }) {
  return (
    <Dialog
      open={dialog.open}
      onOpenChange={(v) => !v && onClose()}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            Install {dialog.name} {dialog.version}
          </DialogTitle>
          <DialogDescription>Answer the configuration questions below.</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit}>
          <div className="space-y-4 py-4">
            {dialog.questions &&
              Object.entries(dialog.questions).map(
                ([key, question]) => {
                  const fieldError = dialog.fieldErrors?.[key]
                  const cachedValue = dialog.responses?.[key]
                  const isCleared = dialog.clearedFields?.[key]
                  const hasCachedValue = !!cachedValue && !isCleared

                  // Build placeholder: show default or type hint
                  let placeholder
                  if (question.default) {
                    placeholder = `Default: ${question.default}`
                  } else if (question.type === 'duration') {
                    placeholder = 'e.g. 30s, 5m, 2h, 1d'
                  } else if (question.type === 'port') {
                    placeholder = 'Auto-assigned if empty'
                  } else if (question.type === 'hostname') {
                    placeholder = 'Auto-generated if empty'
                  }

                  return (
                    <div key={key} className="space-y-2">
                      <Label htmlFor={key}>{question.query}</Label>
                      {hasCachedValue ? (
                        <div className="flex items-center gap-2">
                          <div className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm font-mono">
                            {question.type === 'secret' ? '********' : cachedValue}
                          </div>
                          <input type="hidden" name={key} value={cachedValue} />
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                                onClick={() => onClearField(key)}
                              >
                                <X className="h-4 w-4" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>Clear to enter a new value</TooltipContent>
                          </Tooltip>
                        </div>
                      ) : (
                        <Input
                          id={key}
                          name={key}
                          type={question.type === 'secret' ? 'password' : 'text'}
                          placeholder={placeholder}
                          defaultValue=""
                          className={fieldError ? 'border-destructive' : ''}
                        />
                      )}
                      {question.default && !hasCachedValue && (
                        <p className="text-xs text-muted-foreground">
                          Default: <span className="font-mono">{question.default}</span>
                        </p>
                      )}
                      {question.type === 'duration' && (
                        <p className="text-xs text-muted-foreground">
                          Duration format: use s (seconds), m (minutes), h (hours), or d (days)
                        </p>
                      )}
                      {fieldError && (
                        <p className="text-sm text-destructive">{fieldError}</p>
                      )}
                    </div>
                  )
                },
              )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              type="button"
              onClick={onClose}
            >
              Cancel
            </Button>
            <Button type="submit">Install</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
