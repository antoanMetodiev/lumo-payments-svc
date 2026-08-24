// Package subscriptions mirrors feather.repositories.PaymentSubscriptionRepository
// + feather.services.SubscriptionService/SubscriptionActivationService +
// feather.controllers.SubscriptionController/InternalSubscriptionController
// — owns reads and writes of the local `subscriptions` table.
package subscriptions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"lumo-payments-svc/internal/models"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const columns = "id, clerk_id, stripe_customer_id, stripe_subscription_id, stripe_price_id, " +
	"status, current_period_end, cancel_at_period_end, created_at, updated_at"

func scan(row pgx.Row) (*models.PaymentSubscription, error) {
	var s models.PaymentSubscription
	err := row.Scan(
		&s.ID, &s.ClerkID, &s.StripeCustomerID, &s.StripeSubscriptionID, &s.StripePriceID,
		&s.Status, &s.CurrentPeriodEnd, &s.CancelAtPeriodEnd, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// FindByClerkID mirrors PaymentSubscriptionRepository.findByClerkId.
func (r *Repository) FindByClerkID(ctx context.Context, clerkID string) (*models.PaymentSubscription, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+columns+" FROM subscriptions WHERE clerk_id = $1", clerkID)
	s, err := scan(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// FindByStripeSubscriptionID mirrors PaymentSubscriptionRepository.findByStripeSubscriptionId.
func (r *Repository) FindByStripeSubscriptionID(ctx context.Context, stripeSubscriptionID string) (*models.PaymentSubscription, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+columns+" FROM subscriptions WHERE stripe_subscription_id = $1", stripeSubscriptionID)
	s, err := scan(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// DeleteByClerkID mirrors PaymentSubscriptionRepository.deleteByClerkId — a
// no-op if this clerkId never had a row.
func (r *Repository) DeleteByClerkID(ctx context.Context, clerkID string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM subscriptions WHERE clerk_id = $1", clerkID)
	return err
}

// DeleteByID mirrors JpaRepository.deleteById, as used by
// SubscriptionActivationService.deleteFailedSubscriptionRow.
func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM subscriptions WHERE id = $1", id)
	return err
}

// Upsert mirrors JpaRepository.save(): inserts a brand-new row when ID is
// the zero UUID (assigning a fresh one), otherwise updates the existing row
// by id. clerk_id and created_at are never touched by the update path —
// mirrors the entity's updatable=false columns.
func (r *Repository) Upsert(ctx context.Context, s *models.PaymentSubscription) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
		_, err := r.pool.Exec(ctx, `
			INSERT INTO subscriptions (id, clerk_id, stripe_customer_id, stripe_subscription_id,
				stripe_price_id, status, current_period_end, cancel_at_period_end, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			s.ID, s.ClerkID, s.StripeCustomerID, s.StripeSubscriptionID, s.StripePriceID,
			s.Status, s.CurrentPeriodEnd, s.CancelAtPeriodEnd, s.CreatedAt, s.UpdatedAt,
		)
		return err
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE subscriptions SET stripe_customer_id=$2, stripe_subscription_id=$3, stripe_price_id=$4,
			status=$5, current_period_end=$6, cancel_at_period_end=$7, updated_at=$8
		WHERE id=$1`,
		s.ID, s.StripeCustomerID, s.StripeSubscriptionID, s.StripePriceID,
		s.Status, s.CurrentPeriodEnd, s.CancelAtPeriodEnd, s.UpdatedAt,
	)
	return err
}
