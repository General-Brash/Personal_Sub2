import { apiClient } from './client'

export interface PlayerInvitationSummary {
  user_id: number
  available: number
  reserved: number
  consumed: number
  total_granted: number
  updated_at: string
}

export interface PlayerInvitationReservation {
  id: number
  inviter_user_id: number
  status: 'reserved' | 'claimed' | 'cancelled' | 'expired'
  expires_at: string
  claimed_user_id?: number
  claimed_at?: string
  cancelled_at?: string
  created_at: string
}

export interface PlayerInvitationCredential {
  token: string
  reservation: PlayerInvitationReservation
}

export interface PlayerInvitationState {
  summary: PlayerInvitationSummary
  reservations: PlayerInvitationReservation[]
}

export async function getMyInvitations(): Promise<PlayerInvitationState> {
  const { data } = await apiClient.get<PlayerInvitationState>('/user/invitations')
  return data
}

export async function createMyInvitationReservation(idempotencyKey: string = crypto.randomUUID()): Promise<PlayerInvitationCredential> {
  const { data } = await apiClient.post<PlayerInvitationCredential>('/user/invitations/reservations', undefined, { headers: { 'Idempotency-Key': idempotencyKey } })
  return data
}

export async function cancelMyInvitationReservation(id: number): Promise<void> {
  await apiClient.post(`/user/invitations/reservations/${id}/cancel`)
}

export const invitationAPI = {
  getMyInvitations,
  createMyInvitationReservation,
  cancelMyInvitationReservation,
}

export default invitationAPI
