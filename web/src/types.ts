export type SubscriptionStatus = 'unlimited' | 'active' | 'expiring' | 'expired' | 'depleted' | 'disabled'

export interface SubscriptionInfo {
  status: SubscriptionStatus
  enabled: boolean
  unlimited: boolean
  expiresAt: string | null
  daysLeft: number
  quotaBytes: number
  usedBytes: number
  trafficPercent: number
  online: boolean
  lastOnline: string | null
  subscriptionUrl: string
  inboundCount: number
}

export interface MeResponse {
  subscription: SubscriptionInfo
  renewal: { enabled: boolean; hasPending: boolean; canRenew: boolean }
  push: { enabled: boolean; vapidPublicKey: string; subscribed: boolean }
}

export interface Plan {
  id: string
  title: string
  days: number
  price: number
}

export interface Requisite {
  label: string
  value: string
  note?: string
}

export interface PlansResponse {
  enabled: boolean
  currency: string
  plans: Plan[]
  requisites: Requisite[]
  paymentNote: string
  hasPending: boolean
}

export type RenewalStatus = 'pending' | 'confirmed' | 'rejected' | 'failed'

export interface RenewalRequest {
  id: number
  email?: string
  planId: string
  planTitle: string
  days: number
  amount: number
  currency: string
  status: RenewalStatus
  appliedDays: number
  expiryBefore: string | null
  expiryAfter: string | null
  createdAt: string
  resolvedAt: string | null
  error?: string
}

export interface RenewResponse {
  request: RenewalRequest
  expiresAt: string | null
}

export interface AdminStats {
  sessions: number
  push: Record<string, number>
  renewals: Record<string, number>
  notifications: number
  pushEnabled: boolean
  vapidPublicKey: string
  plans: number
}
