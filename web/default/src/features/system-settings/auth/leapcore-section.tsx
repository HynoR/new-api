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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const leapcoreSchema = z.object({
  LeapCoreHelperKey: z.string().optional(),
  LeapCoreHelperKeyConfigured: z.boolean().optional(),
})

type LeapCoreFormValues = z.infer<typeof leapcoreSchema>

type LeapCoreSectionProps = {
  defaultValues: LeapCoreFormValues
}

export function LeapCoreSection({ defaultValues }: LeapCoreSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<LeapCoreFormValues>({
    resolver: zodResolver(leapcoreSchema),
    defaultValues: {
      LeapCoreHelperKey: defaultValues.LeapCoreHelperKey ?? '',
      LeapCoreHelperKeyConfigured:
        defaultValues.LeapCoreHelperKeyConfigured ?? false,
    },
  })

  const onSubmit = async (data: LeapCoreFormValues) => {
    const helperKey = data.LeapCoreHelperKey?.trim() ?? ''
    if (!helperKey) {
      toast.info(t('No changes to save'))
      return
    }

    await updateOption.mutateAsync({
      key: 'LeapCoreHelperKey',
      value: helperKey,
    })
    form.reset({ LeapCoreHelperKey: '' })
  }

  const isConfigured = defaultValues.LeapCoreHelperKeyConfigured ?? false

  return (
    <SettingsSection
      title={t('LeapCore')}
      description={t('Configure LeapCore machine registration')}
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(onSubmit)}
          className='space-y-6'
          autoComplete='off'
        >
          <FormField
            control={form.control}
            name='LeapCoreHelperKey'
            render={({ field }) => (
              <FormItem>
                <div className='flex items-center gap-2'>
                  <FormLabel>{t('Helper Key')}</FormLabel>
                  {isConfigured ? (
                    <Badge className='bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'>
                      {t('Configured')}
                    </Badge>
                  ) : (
                    <Badge variant='secondary'>{t('Not configured')}</Badge>
                  )}
                </div>
                <FormControl>
                  <Input
                    type='password'
                    placeholder={t('Set LeapCore helper key')}
                    autoComplete='new-password'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Used to validate X-Helper-Key on LeapCore machine registration requests.'
                  )}
                </FormDescription>
                <FormDescription>
                  {t(
                    'Leave empty to keep existing key. New value will overwrite the previous one.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <Button type='submit' disabled={updateOption.isPending}>
            {updateOption.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
