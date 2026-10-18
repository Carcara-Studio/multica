// Package failover owns fork-specific execution availability policy.
package failover

import (
	"regexp"
	"strings"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// The history classifier intentionally accepts broad words such as "quota".
// Automatic routing needs affirmative billing evidence, not just that label.
var billingExhaustion = regexp.MustCompile(`\b(insufficient[_ ]balance|insufficient_quota|balance is too low|monthly usage limit|credits (are )?exhausted|out of credits|credit balance (is )?(exhausted|depleted)|payment required)\b`)

var transientQuota = regexp.MustCompile(`\b(429|529|rate[_ -]?limits?|too many requests|requests per (minute|second)|tokens per (minute|second)|per[- ]minute|per[- ]second|capacity|overloaded)\b`)

// ReliableQuotaExhaustion is a conservative evidence gate, not another failure
// classifier. The caller must also verify primary task route, current target
// generation, active Agent and compatible fallback in its transition transaction.
// Unknown provider wording does not authorize automatic failover.
func ReliableQuotaExhaustion(reason taskfailure.Reason, diagnostic string) bool {
	if reason != taskfailure.ReasonAgentProviderQuotaLimit {
		return false
	}
	// Respect the upstream classifier's more specific auth/context/config rules,
	// including when an older daemon supplied an inaccurate non-empty label.
	if taskfailure.Classify(diagnostic) != taskfailure.ReasonAgentProviderQuotaLimit {
		return false
	}
	diagnostic = strings.ToLower(diagnostic)
	if transientQuota.MatchString(diagnostic) {
		return false
	}
	return billingExhaustion.MatchString(diagnostic)
}
