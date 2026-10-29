import type { CheckinResult } from '@/api/checkin'
import { compareDecimalAmounts, formatMoneyDisplay } from '@/utils/format'

export function formatCheckinCredit(value: string | null | undefined): string {
  const [integer = '0', fraction = ''] = formatMoneyDisplay(value, 8).split('.')
  return `$${integer}.${fraction.replace(/0+$/, '').padEnd(2, '0')}`
}

export function hasPermanentCheckinReward(value: string | null | undefined): boolean {
  return compareDecimalAmounts(value ?? '0', '0') > 0
}

type TranslateReward = (key: string, values: Record<string, string>) => string

export function checkinSuccessMessage(result: CheckinResult, t: TranslateReward): string {
  const temporary = formatCheckinCredit(result.reward_amount)
  return hasPermanentCheckinReward(result.permanent_reward_amount)
    ? t('checkin.successWithPermanent', {
        temporary,
        permanent: formatCheckinCredit(result.permanent_reward_amount),
      })
    : t('checkin.successTemporary', { temporary })
}
