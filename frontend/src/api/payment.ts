/**
 * User Payment API endpoints
 * Handles payment operations for regular users
 */

import { apiClient } from './client'
import type {
  PaymentConfig,
  SubscriptionPlan,
  MethodLimitsResponse,
  CheckoutInfoResponse,
  MallBalanceSummary,
  MallPurchaseRequest,
  MallPurchaseResult,
  CreateOrderRequest,
  CreateOrderResult,
  OrderType,
  PaymentOrder
} from '@/types/payment'
import type { BasePaginationResponse } from '@/types'
import type { LedgerResponse } from '@/types/finance'

export type PublicOrderType = OrderType | ''

/** Minimal anonymous response returned by /payment/public/orders/verify. */
export interface PublicOrderVerifyResult {
  out_trade_no: string
  status: string
  paid: boolean
  order_type: PublicOrderType
  created_at: string
  expires_at: string
  paid_at?: string
  completed_at?: string
}

/**
 * Full signed response returned by /payment/public/orders/resolve.
 *
 * This mirrors the backend PublicOrderResult rather than the minimal legacy
 * verification payload. The backend exposes order_type/status as strings, so
 * unknown persisted values remain representable and callers must not infer a
 * balance order unless the value is exactly "balance".
 */
export interface PublicOrderResult {
  id: number
  out_trade_no: string
  amount: number
  pay_amount: number
  fee_rate: number
  currency: string
  payment_type: string
  order_type: string
  status: string
  created_at: string
  expires_at: string
  paid_at?: string
  completed_at?: string
  refund_amount: number
  refund_reason?: string
  refund_requested_at?: string
  refund_requested_by?: string
  refund_request_reason?: string
  plan_id?: number
  currency_product_id?: number
  currency_product_name?: string
  currency_product_payment_price?: number
  currency_product_credited_amount?: number
}

export const paymentAPI = {
  /** Get payment configuration (enabled types, limits, etc.) */
  getConfig() {
    return apiClient.get<PaymentConfig>('/payment/config')
  },

  /** Get available subscription plans */
  getPlans() {
    return apiClient.get<SubscriptionPlan[]>('/payment/plans')
  },

  /** Get all checkout page data in a single call */
  getCheckoutInfo() {
    return apiClient.get<CheckoutInfoResponse>('/payment/checkout-info')
  },

  /** Get the mall wallet summary without triggering bank settlement. */
  getMallBalance() {
    return apiClient.get<MallBalanceSummary>('/mall/balance')
  },

  /** Purchase a mall product with internal permanent or temporary credit. */
  purchaseMallProduct(data: MallPurchaseRequest, idempotencyKey: string) {
    return apiClient.post<MallPurchaseResult>('/mall/purchases', data, {
      headers: { 'Idempotency-Key': idempotencyKey },
    })
  },

  /** Get payment method limits and fee rates */
  getLimits() {
    return apiClient.get<MethodLimitsResponse>('/payment/limits')
  },

  /** Create a new payment order */
  createOrder(data: CreateOrderRequest) {
    return apiClient.post<CreateOrderResult>('/payment/orders', data)
  },

  /** Get current user's orders */
  getMyOrders(params?: { page?: number; page_size?: number; status?: string }) {
    return apiClient.get<BasePaginationResponse<PaymentOrder>>('/payment/orders/my', { params })
  },

  /** Get the current user's consolidated personal ledger. */
  getLedger(params?: { page?: number; days?: 1 | 7 | 15 }) {
    return apiClient.get<LedgerResponse>('/user/ledger', {
      params: { page: params?.page ?? 1, page_size: 20, days: params?.days },
    })
  },

  /** Get a specific order by ID */
  getOrder(id: number) {
    return apiClient.get<PaymentOrder>(`/payment/orders/${id}`)
  },

  /** Cancel a pending order */
  cancelOrder(id: number) {
    return apiClient.post(`/payment/orders/${id}/cancel`)
  },

  /** Verify order payment status with upstream provider */
  verifyOrder(outTradeNo: string) {
    return apiClient.post<PaymentOrder>('/payment/orders/verify', { out_trade_no: outTradeNo })
  },

  /** Legacy-compatible public order lookup by out_trade_no */
  verifyOrderPublic(outTradeNo: string) {
    return apiClient.post<PublicOrderVerifyResult>('/payment/public/orders/verify', { out_trade_no: outTradeNo })
  },

  /** Resolve an order from a signed resume token without auth */
  resolveOrderPublicByResumeToken(resumeToken: string) {
    return apiClient.post<PublicOrderResult>('/payment/public/orders/resolve', { resume_token: resumeToken })
  },

  /** Request a refund for a completed order */
  requestRefund(id: number, data: { reason: string }) {
    return apiClient.post(`/payment/orders/${id}/refund-request`, data)
  },

  /** Get provider instance IDs that allow user refund */
  getRefundEligibleProviders() {
    return apiClient.get<{ provider_instance_ids: string[] }>('/payment/orders/refund-eligible-providers')
  }
}
