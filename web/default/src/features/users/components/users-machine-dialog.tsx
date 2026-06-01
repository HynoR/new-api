/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { type FormEvent, useRef, useState } from 'react'
import { Check, Copy, Cpu } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { createLeapCoreMachineUser } from '../api'
import { ERROR_MESSAGES } from '../constants'
import { type LeapCoreMachineUser } from '../types'
import { useUsers } from './users-provider'

type UsersMachineDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function CopyButton({ value }: { value: string }) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className='h-8 w-8 shrink-0 p-0'
      onClick={() => copyToClipboard(value)}
      title={t('Copy to clipboard')}
    >
      {copiedText === value ? (
        <Check className='size-4 text-green-600' />
      ) : (
        <Copy className='size-4' />
      )}
    </Button>
  )
}

export function UsersMachineDialog({
  open,
  onOpenChange,
}: UsersMachineDialogProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [machineID, setMachineID] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [result, setResult] = useState<LeapCoreMachineUser | null>(null)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const handleOpenChange = (nextOpen: boolean) => {
    onOpenChange(nextOpen)
    if (!nextOpen) {
      setMachineID('')
      setResult(null)
      setIsSubmitting(false)
    }
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const input = machineID.trim()
    if (!input) {
      toast.error(t('Machine code is required'))
      return
    }

    setResult(null)
    setIsSubmitting(true)
    try {
      const response = await createLeapCoreMachineUser({ machine_id: input })
      if (response.success && response.data) {
        setResult(response.data)
        triggerRefresh()
        toast.success(
          response.data.created
            ? t('Machine user created')
            : t('Machine user already exists')
        )
      } else {
        toast.error(response.message || t(ERROR_MESSAGES.CREATE_FAILED))
      }
    } catch (_error) {
      toast.error(t(ERROR_MESSAGES.UNEXPECTED))
    } finally {
      setIsSubmitting(false)
    }
  }

  const handleProvisionAnother = () => {
    setMachineID('')
    setResult(null)
    setTimeout(() => textareaRef.current?.focus(), 0)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Cpu className='h-4 w-4' />
            {t('Register Machine')}
          </DialogTitle>
          <DialogDescription>
            {t('Create a pre-provisioned LeapCore machine user.')}
          </DialogDescription>
        </DialogHeader>

        <form
          id='leapcore-machine-form'
          className='space-y-4'
          onSubmit={handleSubmit}
        >
          <div className='space-y-2'>
            <Label htmlFor='leapcore-machine-id'>{t('Machine Code')}</Label>
            <Textarea
              ref={textareaRef}
              id='leapcore-machine-id'
              value={machineID}
              onChange={(event) => setMachineID(event.target.value)}
              placeholder={t(
                'Enter fingerprint, machine_id, or base64(machine_id)'
              )}
              rows={4}
              disabled={isSubmitting}
            />
          </div>

          {result && (
            <div className='space-y-3 rounded-md border p-3'>
              <div className='flex items-center gap-2'>
                {result.created ? (
                  <Badge className='bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'>
                    {t('Newly created')}
                  </Badge>
                ) : (
                  <Badge variant='secondary'>{t('Already exists')}</Badge>
                )}
              </div>

              <Alert>
                <AlertDescription>
                  {t(
                    'This initial password is shown only once. Save it before closing this dialog.'
                  )}
                </AlertDescription>
              </Alert>

              <div className='space-y-1.5'>
                <Label htmlFor='leapcore-machine-username'>
                  {t('Username')}
                </Label>
                <div className='flex items-center gap-1'>
                  <Input
                    id='leapcore-machine-username'
                    value={result.username}
                    readOnly
                    className='flex-1'
                  />
                  <CopyButton value={result.username} />
                </div>
                {(result.display_name || result.group) && (
                  <p className='text-muted-foreground text-xs'>
                    {[result.display_name, result.group]
                      .filter(Boolean)
                      .join(' · ')}
                  </p>
                )}
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='leapcore-machine-password'>
                  {t('Initial Password')}
                </Label>
                <div className='flex items-center gap-1'>
                  <Input
                    id='leapcore-machine-password'
                    value={result.password}
                    readOnly
                    className='flex-1'
                  />
                  <CopyButton value={result.password} />
                </div>
              </div>
              <div className='space-y-1.5'>
                <Label htmlFor='leapcore-machine-remark'>{t('Remark')}</Label>
                <div className='flex items-center gap-1'>
                  <Input
                    id='leapcore-machine-remark'
                    value={result.machine_id}
                    readOnly
                    className='flex-1'
                  />
                  <CopyButton value={result.machine_id} />
                </div>
              </div>
            </div>
          )}
        </form>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => handleOpenChange(false)}
          >
            {t('Close')}
          </Button>
          {result ? (
            <Button
              type='button'
              variant='secondary'
              onClick={handleProvisionAnother}
            >
              {t('Provision Another')}
            </Button>
          ) : (
            <Button
              form='leapcore-machine-form'
              type='submit'
              disabled={isSubmitting}
            >
              {isSubmitting ? t('Saving...') : t('Create')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
