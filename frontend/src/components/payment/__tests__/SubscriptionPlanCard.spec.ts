import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";
import type { SubscriptionPlan } from "@/types/payment";

const i18n = createI18n({
  legacy: false,
  locale: "en",
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      commerce: {
        creditType: { permanent: "permanent credit", temporary: "temporary credit" },
        subscription: {
          dailyTemporaryBadge: "Daily temporary credit",
          dailyAmount: "Per day",
          duration: "Duration",
          daysCount: "{days} days",
          totalAmount: "Total credit",
          renewalExtends: "Renewals extend instead of stacking.",
        },
      },
      payment: {
        days: "days",
        models: "Models",
        planCard: {
          quota: "Quota",
          rate: "Rate",
          unlimited: "Unlimited",
        },
        purchaseCard: {
          expectedReceive: "Expected access",
          expectedSpend: "Estimated spend",
        },
        purchaseLimit: {
          ariaLabel: "Purchase limits",
          daily: "Today",
          total: "Total",
          exhausted: "Purchase limit reached",
          detail: "{scope} {used}/{limit}, {remaining} remaining",
        },
        subscribeNow: "Subscribe now",
      },
    },
  },
});

const mountPlanCard = (groupPlatform: string, planOverrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...planOverrides,
      },
    },
    global: { plugins: [i18n, createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it("does not show Antigravity model scopes for OpenAI plans", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");
  });

  it("shows model scopes for Antigravity plans", () => {
    const text = mountPlanCard("antigravity").text();

    expect(text).toContain("Claude");
    expect(text).toContain("Gemini");
    expect(text).toContain("Imagen");
  });

  it("shows plan prices and quota amounts with two decimal places", () => {
    const text = mountPlanCard("openai", {
      price: 10.126,
      original_price: 12.345,
      daily_limit_usd: 1.234,
      weekly_limit_usd: 20.005,
      monthly_limit_usd: 100.999,
    }).text();

    expect(text).toContain("$10.13");
    expect(text).toContain("$12.35");
    expect(text).toContain("$1.23");
    expect(text).toContain("$20.01");
    expect(text).toContain("$101.00");
  });

  it("shows separate daily and total purchase progress badges", () => {
    const wrapper = mountPlanCard("openai", {
      daily_purchase_limit: 2,
      daily_purchase_remaining: 1,
      total_purchase_limit: 5,
      total_purchase_remaining: 3,
    });

    expect(wrapper.get('[data-test="purchase-limit-daily"]').text()).toContain('1/2');
    expect(wrapper.get('[data-test="purchase-limit-total"]').text()).toContain('2/5');
  });

  it("disables purchase when either finite limit is exhausted", () => {
    const wrapper = mountPlanCard("openai", {
      daily_purchase_limit: 1,
      daily_purchase_remaining: 0,
    });

    const button = wrapper.get('[data-test="subscription-plan-select"]');
    expect(button.attributes('disabled')).toBeDefined();
    expect(button.text()).toContain('payment.purchaseLimit.exhausted');
  });

  it("renders daily temporary-credit benefit, duration, total and payment credit type", () => {
    const wrapper = mountPlanCard("", {
      benefit_type: 'daily_temporary_credit',
      payment_credit_type: 'temporary',
      daily_temporary_credit_amount: 10,
      validity_days: 3,
      price: 2,
    });

    const text = wrapper.text();
    expect(text).toContain('commerce.subscription.dailyTemporaryBadge');
    expect(text).toContain('$10.00');
    expect(text).toContain('commerce.subscription.daysCount');
    expect(text).toContain('$30.00');
    expect(text).toContain('commerce.creditType.temporary');
    expect(text).toContain('commerce.subscription.renewalExtends');
  });

  it.each([
    ['days', 2, '2payment.days'],
    ['weeks', 2, '14payment.days'],
    ['months', 2, '60payment.days'],
  ])('renders legacy %s validity using backend-equivalent days', (validityUnit, validityDays, expected) => {
    const text = mountPlanCard('openai', {
      validity_days: validityDays,
      validity_unit: validityUnit,
    }).text();

    expect(text).toContain(expected);
  });

  it("shows an explicit balance reason when purchase is disabled", () => {
    const wrapper = mount(SubscriptionPlanCard, {
      props: {
        plan: mountPlanCard("openai").props('plan'),
        disabledReason: 'Insufficient permanent credit',
      },
      global: { plugins: [i18n, createPinia()] },
    });

    const button = wrapper.get('[data-test="subscription-plan-select"]');
    expect(button.attributes('disabled')).toBeDefined();
    expect(button.text()).toContain('Insufficient permanent credit');
  });
});
