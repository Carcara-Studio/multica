package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChannelMediaClaimReservesOnlyReturnedRowWithNestedLoop(t *testing.T) {
	ctx := context.Background()
	pool := sharedTestPool(t)
	fx := testutil.New(pool, uuid.NewString(), "")
	first, second := "claim-first-"+uuid.NewString(), "claim-second-"+uuid.NewString()
	for _, key := range []string{first, second} {
		// Fresh public fixtures cannot be claimed by another package's sweep.
		fx.InsertNoID(t, "channel_media_pending_object", testutil.Cols{
			"storage_key": key, "workspace_id": fx.WorkspaceID,
			"chat_message_id": uuid.NewString(), "storage_url": "https://claim.test/object",
		}, "storage_key = $1", key)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Shadow the shared ledger so statistics, planner settings and leases are
	// isolated, while retaining the real schema, defaults and indexes.
	for _, statement := range []string{
		"CREATE TEMP TABLE channel_media_pending_object (LIKE public.channel_media_pending_object INCLUDING ALL) ON COMMIT DROP",
		"SET LOCAL search_path = pg_temp, public",
		"SET LOCAL enable_material = off",
		"SET LOCAL enable_hashjoin = off",
		"SET LOCAL enable_mergejoin = off",
		"SET LOCAL enable_indexscan = off",
		"SET LOCAL enable_bitmapscan = off",
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	copyDue := func(key, age string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO pg_temp.channel_media_pending_object
			SELECT * FROM public.channel_media_pending_object WHERE storage_key = $1`, key); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `UPDATE pg_temp.channel_media_pending_object
			SET created_at = now() - $2::interval, next_attempt_at = now() - $2::interval
			WHERE storage_key = $1`, key, age); err != nil {
			t.Fatal(err)
		}
	}
	copyDue(first, "2 hours")
	if _, err := tx.Exec(ctx, "ANALYZE pg_temp.channel_media_pending_object"); err != nil {
		t.Fatal(err)
	}
	// Stale one-row statistics allow the target to become the outer side of
	// a nested loop. An inline LIMIT subquery can then run once per target row.
	copyDue(second, "1 hour")
	q := db.New(tx)
	claim := func(key string) pgtype.UUID {
		t.Helper()
		token := util.MustParseUUID(uuid.NewString())
		row, err := q.ClaimNextChannelMediaPendingObjectForReconcile(ctx, db.ClaimNextChannelMediaPendingObjectForReconcileParams{
			LeaseToken: token, Lease: pgInterval(channelMediaReconcileLease), SettleDelay: pgInterval(ChannelMediaReconcileSettleDelay),
		})
		if err != nil || row.StorageKey != key || row.LeaseToken != token || row.Attempt != 1 {
			t.Fatalf("claim %s returned %+v: %v", key, row, err)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM channel_media_pending_object WHERE lease_token = $1", token).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("one receipt reserved %d ledger rows; want exactly one", count)
		}
		return token
	}
	firstToken := claim(first)
	var state string
	var attempt int
	var token pgtype.UUID
	var expires pgtype.Timestamptz
	if err := tx.QueryRow(ctx, `SELECT state, attempt, lease_token, lease_expires_at
		FROM channel_media_pending_object WHERE storage_key = $1`, second).Scan(&state, &attempt, &token, &expires); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempt != 0 || token.Valid || expires.Valid {
		t.Fatalf("unreturned row was reserved: state=%s attempt=%d token=%v expires=%v", state, attempt, token, expires)
	}
	secondToken := claim(second)
	if firstToken == secondToken {
		t.Fatal("separate claims reused a lease token")
	}
	if _, err := q.ClaimNextChannelMediaPendingObjectForReconcile(ctx, db.ClaimNextChannelMediaPendingObjectForReconcileParams{
		LeaseToken: util.MustParseUUID(uuid.NewString()), Lease: pgInterval(channelMediaReconcileLease), SettleDelay: pgInterval(ChannelMediaReconcileSettleDelay),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unexpired leases became claimable: %v", err)
	}
}
