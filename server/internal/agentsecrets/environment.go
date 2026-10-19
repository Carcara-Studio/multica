package agentsecrets

import (
	"sort"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Sentinel round-trips masked environment values without persisting the mask.
const Sentinel = "****"

// CanManage is the existing Agent secret-management ownership predicate.
// Membership and authenticated human actor checks belong to the caller.
func CanManage(a db.Agent, member db.Member) bool {
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	return a.OwnerID.Valid && member.UserID.Valid && a.OwnerID.Bytes == member.UserID.Bytes
}

// EnvironmentAudit summarises the diff between an agent's existing env and the
// new one, broken down so an auditor can reconstruct exactly which
// keys an operation touched without leaking values. All slices are
// sorted to keep the activity row content deterministic for tests and
// downstream tooling.
type EnvironmentAudit struct {
	Added     []string
	Removed   []string
	Changed   []string
	Preserved []string
}

// MergeEnvironment applies the **** sentinel rule and returns both the
// final map to persist and an audit summary of which keys changed.
// Behaviour:
//   - request key present, value == "****", key exists in `existing`
//     → keep the existing value, append to preserved
//   - request key present, value == "****", key NOT in `existing`
//     → drop the key (literal "****" is never a valid stored value)
//   - request key present, value != "****", key already in existing
//     with same value → no-op (not counted)
//   - request key present, value != "****", different from existing
//     → write new value, append to changed
//   - request key present, value != "****", key NOT in existing
//     → write new value, append to added
//   - key in existing but absent from request → removed
func MergeEnvironment(existing, request map[string]string) (map[string]string, EnvironmentAudit) {
	merged := make(map[string]string, len(request))
	audit := EnvironmentAudit{}

	for k, v := range request {
		if v == Sentinel {
			if old, ok := existing[k]; ok {
				merged[k] = old
				audit.Preserved = append(audit.Preserved, k)
			}
			// else: drop. We never persist a literal "****".
			continue
		}
		if old, ok := existing[k]; ok {
			if old == v {
				merged[k] = v
				continue
			}
			merged[k] = v
			audit.Changed = append(audit.Changed, k)
			continue
		}
		merged[k] = v
		audit.Added = append(audit.Added, k)
	}

	for k := range existing {
		if _, ok := request[k]; !ok {
			audit.Removed = append(audit.Removed, k)
		}
	}

	sort.Strings(audit.Added)
	sort.Strings(audit.Removed)
	sort.Strings(audit.Changed)
	sort.Strings(audit.Preserved)
	return merged, audit
}
