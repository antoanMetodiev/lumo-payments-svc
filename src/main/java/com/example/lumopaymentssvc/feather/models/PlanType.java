package com.example.lumopaymentssvc.feather.models;

/**
 * What the frontend/lumo-user-svc actually care about — mirrors
 * lumo-user-svc's SubscriptionType (REGULAR_USER / PRO_USER), just named for
 * this service's own domain (Stripe status -> plan, not a DB-persisted enum).
 */
public enum PlanType {
    FREE,
    PRO
}
