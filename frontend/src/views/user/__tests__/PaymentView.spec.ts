import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, shallowMount } from '@vue/test-utils'
import PaymentView from '../PaymentView.vue'
import { PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import type { CheckoutInfoResponse, MethodLimit, SubscriptionPlan } from '@/types/payment'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'

const routeState = vi.hoisted(() => ({
  path: '/mall',
  query: {} as Record<string, unknown>,
}))

const routerReplace = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
const routerResolve = vi.hoisted(() => vi.fn(() => ({ href: '/payment/stripe?mock=1' })))
const createOrder = vi.hoisted(() => vi.fn())
const purchaseMallProduct = vi.hoisted(() => vi.fn())
const refreshUser = vi.hoisted(() => vi.fn())
const fetchActiveSubscriptions = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))
const showError = vi.hoisted(() => vi.fn())
const showInfo = vi.hoisted(() => vi.fn())
const showWarning = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())
const getCheckoutInfo = vi.hoisted(() => vi.fn())
const bridgeInvoke = vi.hoisted(() => vi.fn())
const appState = vi.hoisted(() => ({
  cachedPublicSettings: { payment_enabled: true } as { payment_enabled?: boolean },
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
    useRouter: () => ({
      replace: routerReplace,
      push: routerPush,
      resolve: routerResolve,
    }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'payment.purchaseConfirm.subscriptionReceive') return String(params?.validity ?? '')
        return key
      },
    }),
  }
})

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: {
      username: 'demo-user',
      balance: 0,
    },
    refreshUser,
  }),
}))

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    createOrder,
  }),
}))

vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({
    activeSubscriptions: [],
    fetchActiveSubscriptions,
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ ...appState, showError, showInfo, showWarning, showSuccess }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    getCheckoutInfo,
    purchaseMallProduct,
  },
}))

vi.mock('@/utils/device', () => ({
  isMobileDevice: () => true,
}))

const PurchaseConfirmStub = {
  name: 'ProductPurchaseConfirmDialog',
  props: ['show', 'productName', 'description', 'paymentMethod', 'expectedSpend', 'expectedReceive', 'limits', 'submitting'],
  emits: ['close', 'confirm'],
  template: '<div v-if="show" data-test="purchase-confirm-dialog"><button type="button" data-test="purchase-confirm-submit" @click="$emit(\'confirm\')">confirm</button></div>',
}

beforeEach(() => {
  appState.cachedPublicSettings = { payment_enabled: true }
})

function checkoutInfoFixture(overrides: Partial<CheckoutInfoResponse> = {}) {
  const wxpayMethod: MethodLimit = {
    daily_limit: 0,
    daily_used: 0,
    daily_remaining: 0,
    single_min: 0,
    single_max: 0,
    fee_rate: 0,
    available: true,
  }
  const data: CheckoutInfoResponse = {
    methods: {
      wxpay: wxpayMethod,
    },
    global_min: 0,
    global_max: 0,
    plans: [],
    balance: {
      permanent_balance: '100.00000000',
      temporary_credit_available: '25.00000000',
    },
    balance_disabled: false,
    balance_recharge_multiplier: 1,
    subscription_usd_to_cny_rate: 0,
    recharge_fee_rate: 0,
    help_text: '',
    help_image_url: '',
    stripe_publishable_key: '',
  }

  return {
    data: { ...data, ...overrides },
  }
}

function checkoutInfoWithPlansFixture(options: {
  checkout?: Partial<CheckoutInfoResponse>
  method?: Partial<MethodLimit>
  plan?: Partial<SubscriptionPlan>
  plans?: SubscriptionPlan[]
} = {}) {
  const base = checkoutInfoFixture(options.checkout).data
  const plan: SubscriptionPlan = {
    id: 7,
    group_id: 3,
    name: 'Starter',
    description: '',
    price: 128,
    original_price: 0,
    validity_days: 30,
    validity_unit: 'day',
    rate_multiplier: 1,
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    features: [],
    group_platform: 'openai',
    sort_order: 1,
    for_sale: true,
    group_name: 'OpenAI',
    ...options.plan,
  }

  return {
    data: {
      ...base,
      methods: {
        ...base.methods,
        wxpay: {
          ...base.methods.wxpay,
          ...options.method,
        },
      },
      plans: options.plans ?? [plan],
    },
  }
}

function jsapiOrderFixture(resumeToken: string) {
  return {
    order_id: 123,
    amount: 88,
    pay_amount: 88,
    fee_rate: 0,
    expires_at: '2099-01-01T00:10:00.000Z',
    payment_type: 'wxpay',
    out_trade_no: 'sub2_jsapi_123',
    result_type: 'jsapi_ready' as const,
    resume_token: resumeToken,
    jsapi: {
      appId: 'wx123',
      timeStamp: '1712345678',
      nonceStr: 'nonce',
      package: 'prepay_id=wx123',
      signType: 'RSA',
      paySign: 'signed',
    },
  }
}

function oauthOrderFixture() {
  return {
    order_id: 456,
    amount: 128,
    pay_amount: 128,
    fee_rate: 0,
    expires_at: '2099-01-01T00:10:00.000Z',
    payment_type: 'wxpay',
    result_type: 'oauth_required' as const,
    oauth: {
      authorize_url: '/api/v1/auth/oauth/wechat/payment/start?payment_type=wxpay&redirect=%2Fmall%3Ffrom%3Dwechat',
      appid: 'wx123',
      scope: 'snsapi_base',
      redirect_url: '/auth/wechat/payment/callback',
    },
  }
}

async function mountSubscriptionConfirm(options: Parameters<typeof checkoutInfoWithPlansFixture>[0] = {}) {
  vi.useRealTimers()
  routeState.path = '/mall'
  routeState.query = {
    tab: 'subscription',
    group: '3',
  }
  routerReplace.mockReset().mockResolvedValue(undefined)
  routerPush.mockReset().mockResolvedValue(undefined)
  routerResolve.mockClear()
  createOrder.mockReset()
  purchaseMallProduct.mockReset()
  refreshUser.mockReset()
  fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
  showError.mockReset()
  showInfo.mockReset()
  showWarning.mockReset()
  showSuccess.mockReset()
  getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoWithPlansFixture(options))
  bridgeInvoke.mockReset()
  window.localStorage.clear()
  ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined

  const wrapper = shallowMount(PaymentView, {
    global: {
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
        Teleport: true,
        Transition: false,
        RouterLink: true,
        ProductPurchaseConfirmDialog: PurchaseConfirmStub,
      },
    },
  })
  await flushPromises()
  await flushPromises()
  return wrapper
}

async function mountStoreTabs(checkoutOverrides: Partial<CheckoutInfoResponse> = {}) {
  vi.useRealTimers()
  routeState.path = '/mall'
  routeState.query = {}
  routerReplace.mockReset().mockResolvedValue(undefined)
  routerPush.mockReset().mockResolvedValue(undefined)
  createOrder.mockReset()
  purchaseMallProduct.mockReset()
  refreshUser.mockReset()
  fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
  showError.mockReset()
  showInfo.mockReset()
  showWarning.mockReset()
  showSuccess.mockReset()
  getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture(checkoutOverrides))
  window.localStorage.clear()

  const wrapper = shallowMount(PaymentView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Teleport: true,
        Transition: false,
        RouterLink: true,
        ProductPurchaseConfirmDialog: PurchaseConfirmStub,
      },
    },
  })
  await flushPromises()
  await flushPromises()
  return wrapper
}

describe('PaymentView unified store layout', () => {
  it('shows currency and subscription sections together without a top-level switcher', async () => {
    const wrapper = await mountStoreTabs()
    const rechargePanel = wrapper.get('#store-panel-recharge')
    const subscriptionPanel = wrapper.get('#store-panel-subscription')

    expect(rechargePanel.attributes('hidden')).toBeUndefined()
    expect(subscriptionPanel.attributes('hidden')).toBeUndefined()
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(
      rechargePanel.element.compareDocumentPosition(subscriptionPanel.element)
        & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
    wrapper.unmount()
  })

  it('keeps internal mall purchases available when no provider is configured', async () => {
    appState.cachedPublicSettings = { payment_enabled: false }
    const currencyWrapper = await mountStoreTabs({
      methods: {},
      balance_disabled: true,
      currency_products: [{
        id: 21,
        name: 'Internal credit',
        description: '',
        payment_price: 10,
        payment_credit_type: 'permanent',
        credited_type: 'temporary',
        credited_amount: 12,
        sort_order: 1,
      }],
    })

    expect(currencyWrapper.get('#store-panel-recharge').attributes('hidden')).toBeUndefined()
    expect(currencyWrapper.get('#store-panel-subscription').attributes('hidden')).toBeUndefined()
    await currencyWrapper.get('[data-test="currency-product-21"]').trigger('click')
    expect(currencyWrapper.get('[data-test="purchase-confirm-dialog"]').exists()).toBe(true)
    currencyWrapper.unmount()

    const subscriptionWrapper = await mountSubscriptionConfirm({
      checkout: { methods: {} },
      plan: { price: 12 },
    })
    expect(subscriptionWrapper.get('[data-test="purchase-confirm-dialog"]').exists()).toBe(true)
  })
})

describe('PaymentView internal subscription purchases', () => {
  it('purchases only after final confirmation without requiring a provider', async () => {
    const wrapper = await mountSubscriptionConfirm({
      checkout: { methods: {} },
      plan: { price: 12, payment_credit_type: 'temporary' },
    })
    purchaseMallProduct.mockResolvedValue({
      data: {
        purchase_id: 902,
        product_type: 'subscription',
        product_id: 7,
        payment_credit_type: 'temporary',
        price: '12.00000000',
        benefit_type: 'sub2',
        permanent_balance: '100.00000000',
        temporary_credit_available: '13.00000000',
      },
    })

    expect(purchaseMallProduct).not.toHaveBeenCalled()
    const confirmButton = wrapper.get('[data-test="purchase-confirm-submit"]')
    await Promise.all([confirmButton.trigger('click'), confirmButton.trigger('click')])
    await flushPromises()

    expect(purchaseMallProduct).toHaveBeenCalledWith(
      { product_type: 'subscription', product_id: 7 },
      expect.stringMatching(/^mall-subscription-7-/),
    )
    expect(createOrder).not.toHaveBeenCalled()
  })

  it.each([
    ['days', 2, '2payment.days'],
    ['weeks', 2, '14payment.days'],
    ['months', 2, '60payment.days'],
  ])('shows backend-equivalent validity for legacy %s plans', async (validityUnit, validityDays, expected) => {
    const wrapper = await mountSubscriptionConfirm({
      plan: {
        price: 12,
        validity_days: validityDays,
        validity_unit: validityUnit,
      },
    })

    expect(wrapper.getComponent(PurchaseConfirmStub).props('expectedReceive')).toContain(expected)
  })

  it('shows the disabled balance reason in the renewal plan picker', async () => {
    const firstPlan = checkoutInfoWithPlansFixture().data.plans[0]
    const wrapper = await mountSubscriptionConfirm({
      checkout: {
        balance: {
          permanent_balance: '0.00000000',
          temporary_credit_available: '0.00000000',
        },
      },
      plans: [firstPlan, { ...firstPlan, id: 8, name: 'Starter renewal' }],
    })

    const cards = wrapper.findAllComponents(SubscriptionPlanCard)
    expect(cards).toHaveLength(4)
    expect(cards.every(card => card.props('disabledReason') === 'commerce.purchase.insufficient.permanent')).toBe(true)
  })
})

describe('PaymentView payment recovery', () => {
  beforeEach(() => {
    vi.useRealTimers()
    routeState.path = '/mall'
    routeState.query = {}
    routerReplace.mockReset().mockResolvedValue(undefined)
    routerPush.mockReset().mockResolvedValue(undefined)
    routerResolve.mockClear()
    createOrder.mockReset()
    purchaseMallProduct.mockReset()
    refreshUser.mockReset()
    fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
    showError.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    bridgeInvoke.mockReset()
    window.localStorage.clear()
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined
  })

  it('opens a fixed currency-product confirmation immediately and purchases by server-owned id', async () => {
    getCheckoutInfo.mockResolvedValue(checkoutInfoFixture({
      currency_products: [{
        id: 12,
        name: 'Starter credit',
        description: 'A fixed permanent-credit bundle',
        payment_price: 19.9,
        payment_credit_type: 'permanent',
        credited_type: 'temporary',
        credited_amount: 25,
        sort_order: 1,
        daily_purchase_limit: 1,
        daily_purchase_remaining: 1,
        total_purchase_limit: 3,
        total_purchase_remaining: 2,
      }],
    }))
    purchaseMallProduct.mockResolvedValue({
      data: {
        purchase_id: 901,
        product_type: 'currency',
        product_id: 12,
        payment_credit_type: 'permanent',
        price: '19.90000000',
        credited_type: 'temporary',
        credited_amount: '25.00000000',
        permanent_balance: '80.10000000',
        temporary_credit_available: '50.00000000',
      },
    })
    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          PaymentStatusPanel: true,
          PaymentMethodSelector: true,
          AmountInput: true,
          Teleport: true,
          Transition: false,
          RouterLink: true,
          ProductPurchaseConfirmDialog: PurchaseConfirmStub,
        },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="currency-product-12"]').trigger('click')
    expect(purchaseMallProduct).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="purchase-confirm-dialog"]').exists()).toBe(true)
    await wrapper.get('[data-test="purchase-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(purchaseMallProduct).toHaveBeenCalledWith(
      { product_type: 'currency', product_id: 12 },
      expect.stringMatching(/^mall-currency-12-/),
    )
    expect(createOrder).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="mall-permanent-balance"]').text()).toContain('$80.10')
    expect(wrapper.get('[data-test="mall-temporary-balance"]').text()).toContain('$50.00')
  })

  it('shows both mall balances and a clear insufficient-credit reason', async () => {
    getCheckoutInfo.mockResolvedValue(checkoutInfoFixture({
      methods: {},
      balance: {
        permanent_balance: '4.00000000',
        temporary_credit_available: '2.50000000',
      },
      currency_products: [{
        id: 13,
        name: 'Too expensive',
        description: '',
        payment_price: 5,
        payment_credit_type: 'temporary',
        credited_type: 'permanent',
        credited_amount: 10,
        sort_order: 1,
      }],
    }))
    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Teleport: true,
          Transition: false,
          RouterLink: true,
          ProductPurchaseConfirmDialog: PurchaseConfirmStub,
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-test="mall-permanent-balance"]').text()).toContain('$4.00')
    expect(wrapper.get('[data-test="mall-temporary-balance"]').text()).toContain('$2.50')
    expect(wrapper.get('[data-test="currency-product-13"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="currency-product-disabled-13"]').text()).toBe('commerce.purchase.insufficient.temporary')
  })

  it.each([
    ['permanent', 'permanent'],
    ['permanent', 'temporary'],
    ['temporary', 'permanent'],
    ['temporary', 'temporary'],
  ] as const)('shows both credit types for %s payment and %s receipt', async (paymentCreditType, creditedType) => {
    getCheckoutInfo.mockResolvedValue(checkoutInfoFixture({
      currency_products: [{
        id: 30,
        name: 'Mixed credit product',
        description: '',
        payment_price: 1,
        payment_credit_type: paymentCreditType,
        credited_type: creditedType,
        credited_amount: 2,
        sort_order: 1,
      }],
    }))
    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Teleport: true,
          Transition: false,
          RouterLink: true,
          ProductPurchaseConfirmDialog: PurchaseConfirmStub,
        },
      },
    })
    await flushPromises()
    await wrapper.get('[data-test="currency-product-30"]').trigger('click')

    const dialog = wrapper.getComponent(PurchaseConfirmStub)
    expect(dialog.props('expectedSpend')).toContain(`commerce.creditType.${paymentCreditType}`)
    expect(dialog.props('expectedReceive')).toContain(`commerce.creditType.${creditedType}`)
  })

  it('restores legacy EasyPay recovery without exposing provider controls in the mall', async () => {
    getCheckoutInfo.mockResolvedValue(checkoutInfoFixture({
      methods: {
        wxpay: checkoutInfoFixture().data.methods.wxpay,
        ldc: {
          daily_limit: 0,
          daily_used: 0,
          daily_remaining: 0,
          single_min: 0,
          single_max: 0,
          fee_rate: 0,
          available: true,
          display_name: 'LDC Pay',
        },
      },
    }))
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 888,
      amount: 66,
      qrCode: 'ldc-qr',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'ldc',
      payUrl: 'https://pay.example.com/ldc',
      outTradeNo: 'sub2_ldc_888',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 66,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: '',
      createdAt: Date.now(),
    }))

    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          AppLayout: {
            template: '<div><slot /></div>',
          },
          PaymentStatusPanel: {
            template: '<button data-test="payment-done" @click="$emit(\'done\')" />',
          },
          PaymentMethodSelector: {
            props: ['selected'],
            template: '<div data-test="method-selector">{{ selected }}</div>',
          },
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()
    await wrapper.find('[data-test="payment-done"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="method-selector"]').exists()).toBe(false)
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })
})

describe('PaymentView WeChat JSAPI flow', () => {
  beforeEach(() => {
    routeState.path = '/mall'
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-token-123',
    }
    routerReplace.mockReset().mockResolvedValue(undefined)
    routerPush.mockReset().mockResolvedValue(undefined)
    routerResolve.mockClear()
    createOrder.mockReset()
    refreshUser.mockReset()
    fetchActiveSubscriptions.mockReset().mockResolvedValue(undefined)
    showError.mockReset()
    showInfo.mockReset()
    showWarning.mockReset()
    getCheckoutInfo.mockReset().mockResolvedValue(checkoutInfoFixture())
    bridgeInvoke.mockReset()
    window.localStorage.clear()
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = {
      invoke: bridgeInvoke,
    }
  })

  it('resets payment state and redirects to /payment/result after JSAPI reports success', async () => {
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-123'))
    bridgeInvoke.mockImplementation((_action, _payload, callback) => {
      callback({ err_msg: 'get_brand_wcpay_request:ok' })
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(routerReplace).toHaveBeenCalledWith({ path: '/mall', query: {} })
    expect(routerPush).toHaveBeenCalledWith({
      path: '/payment/result',
      query: {
        order_id: '123',
        out_trade_no: 'sub2_jsapi_123',
        resume_token: 'resume-token-123',
      },
    })
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('resets payment state when JSAPI reports cancellation', async () => {
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-cancel'))
    bridgeInvoke.mockImplementation((_action, _payload, callback) => {
      callback({ err_msg: 'get_brand_wcpay_request:cancel' })
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(showInfo).toHaveBeenCalledWith('payment.qr.cancelled')
    expect(routerPush).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('clears stale recovery state when JSAPI never becomes available', async () => {
    vi.useFakeTimers()
    createOrder.mockResolvedValue(jsapiOrderFixture('resume-token-missing-bridge'))
    ;(window as Window & { WeixinJSBridge?: { invoke: typeof bridgeInvoke } }).WeixinJSBridge = undefined

    const wrapper = shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(4000)
    await flushPromises()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      'payment.errors.wechatJsapiUnavailable payment.errors.wechatOpenInWeChatHint',
    )
    expect(routerPush).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
    expect(wrapper.html()).not.toContain('payment-status-panel-stub')
  })

  it('clears a stale recovery snapshot before handling wechat resume callback params', async () => {
    createOrder.mockRejectedValueOnce(new Error('resume failed'))
    window.localStorage.setItem(PAYMENT_RECOVERY_STORAGE_KEY, JSON.stringify({
      orderId: 999,
      amount: 66,
      qrCode: 'stale-qr',
      expiresAt: '2099-01-01T00:10:00.000Z',
      paymentType: 'alipay',
      payUrl: 'https://pay.example.com/stale',
      outTradeNo: 'stale-out-trade-no',
      clientSecret: '',
      intentId: '',
      currency: '',
      countryCode: '',
      paymentEnv: '',
      payAmount: 66,
      orderType: 'balance',
      paymentMode: 'popup',
      resumeToken: '',
      createdAt: Date.UTC(2099, 0, 1, 0, 0, 0),
    }))

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      wechat_resume_token: 'resume-token-123',
    }))
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toBeNull()
  })

  it('keeps subscription resume context for token-only WeChat callbacks', async () => {
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-subscription-7',
      payment_type: 'wxpay_direct',
      order_type: 'subscription',
      plan_id: '7',
    }
    getCheckoutInfo.mockResolvedValue(checkoutInfoWithPlansFixture())
    createOrder.mockResolvedValue(oauthOrderFixture())

    const originalLocation = window.location
    const locationState = {
      href: 'http://localhost/mall',
      origin: 'http://localhost',
    }
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState,
    })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(routerReplace).toHaveBeenCalledWith({ path: '/mall', query: {} })
    expect(createOrder).toHaveBeenCalledWith(expect.objectContaining({
      payment_type: 'wxpay',
      order_type: 'subscription',
      plan_id: 7,
      wechat_resume_token: 'resume-subscription-7',
    }))
    expect(locationState.href).toContain('/api/v1/auth/oauth/wechat/payment/start?')
    expect(new URL(locationState.href, 'http://localhost').searchParams.get('redirect')).toBe(
      '/mall?from=wechat&payment_type=wxpay&order_type=subscription&plan_id=7',
    )

    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    })
  })

  it('falls back to QR flow when mobile WeChat payment is unavailable', async () => {
    routeState.query = {
      wechat_resume: '1',
      wechat_resume_token: 'resume-token-h5',
      payment_type: 'wxpay_direct',
    }
    createOrder
      .mockRejectedValueOnce({ reason: 'WECHAT_H5_NOT_AUTHORIZED' })
      .mockResolvedValueOnce({
        order_id: 778,
        amount: 88,
        pay_amount: 88,
        fee_rate: 0,
        expires_at: '2099-01-01T00:10:00.000Z',
        payment_type: 'wxpay',
        qr_code: 'weixin://wxpay/bizpayurl?pr=fallback-native',
        out_trade_no: 'sub2_qr_778',
      })

    shallowMount(PaymentView, {
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })
    await flushPromises()
    await flushPromises()

    expect(createOrder).toHaveBeenNthCalledWith(1, expect.objectContaining({
      payment_type: 'wxpay',
      is_mobile: true,
      wechat_resume_token: 'resume-token-h5',
    }))
    expect(createOrder).toHaveBeenNthCalledWith(2, expect.objectContaining({
      payment_type: 'wxpay',
      is_mobile: false,
      payment_source: 'hosted_redirect',
    }))
    expect(showWarning).toHaveBeenCalledWith('payment.errors.mobilePaymentFallbackToQr')
    expect(showError).not.toHaveBeenCalled()
    expect(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY)).toContain('weixin://wxpay/bizpayurl?pr=fallback-native')
  })
})
