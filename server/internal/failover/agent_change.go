package failover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentExecutionChanged excludes cosmetic metadata and live idle/working state.
// Inputs are DB round-trips, so jsonb has canonical serialization. Never include
// secret values in fingerprints, logs or errors; only compare in memory.
func AgentExecutionChanged(before, after db.Agent) bool {
	return before.RuntimeID != after.RuntimeID || before.RuntimeMode != after.RuntimeMode || before.OwnerID != after.OwnerID ||
		before.Model != after.Model || before.ThinkingLevel != after.ThinkingLevel || before.ServiceTier != after.ServiceTier ||
		!bytes.Equal(before.RuntimeConfig, after.RuntimeConfig) || !bytes.Equal(before.CustomEnv, after.CustomEnv) ||
		!bytes.Equal(before.CustomArgs, after.CustomArgs) || !bytes.Equal(before.McpConfig, after.McpConfig) ||
		!bytes.Equal(before.DisabledRuntimeSkills, after.DisabledRuntimeSkills) || !slices.Equal(before.ComposioToolkitAllowlist, after.ComposioToolkitAllowlist)
}

// InvalidateAgentChange must run in the Agent mutation's existing WithTx, after
// owner/chat/task/audit writes and before commit. Caller holds the Agent row.
// It is not an enqueue/claim resolver or a recovery proof.
func InvalidateAgentChange(ctx context.Context, q *db.Queries, before, after db.Agent) error {
	if before.ID != after.ID || before.WorkspaceID != after.WorkspaceID {
		return errors.New("failover Agent identity changed")
	}
	if !AgentExecutionChanged(before, after) && before.ArchivedAt == after.ArchivedAt {
		return nil
	}
	return invalidateAgentPolicy(ctx, q, after, after.ArchivedAt.Valid || !after.RuntimeID.Valid)
}

// InvalidateAgentPolicies handles archive/unbind batches whose Agent rows are
// already locked. Call last, after all upstream writes, in stable state order.
// No state is created for ordinary Agents; the input/event order is preserved.
func InvalidateAgentPolicies(ctx context.Context, q *db.Queries, agents []db.Agent, suspend bool) error {
	ordered := slices.Clone(agents)
	slices.SortFunc(ordered, func(a, b db.Agent) int { return bytes.Compare(a.ID.Bytes[:], b.ID.Bytes[:]) })
	for _, a := range ordered {
		if err := invalidateAgentPolicy(ctx, q, a, suspend); err != nil {
			return err
		}
	}
	return nil
}

func invalidateAgentPolicy(ctx context.Context, q *db.Queries, a db.Agent, suspend bool) error {
	state, err := q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load Agent failover state: %w", err)
	}
	generation := pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true}
	if _, err := q.RotateAgentFailoverGeneration(ctx, db.RotateAgentFailoverGenerationParams{
		AgentID: a.ID, WorkspaceID: a.WorkspaceID, ExpectedGeneration: state.PrimaryGeneration, NewGeneration: generation,
	}); err != nil {
		return fmt.Errorf("invalidate Agent failover generation: %w", err)
	}
	if suspend && !state.SuspendedAt.Valid {
		if _, err := q.SuspendAgentFailoverState(ctx, db.SuspendAgentFailoverStateParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID}); err != nil {
			return fmt.Errorf("suspend Agent failover state: %w", err)
		}
	}
	return nil
}
