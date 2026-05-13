// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

// Risk is the three-tier risk taxonomy. Aliased to string (not a defined
// type) so plugin authors can use either the constants below or raw literals
// without conversion friction.
type Risk = string

const (
	RiskRead          Risk = "read"
	RiskWrite         Risk = "write"
	RiskHighRiskWrite Risk = "high-risk-write"
)

// Identity values supported by the framework. Aliased to string for the same
// reason as Risk.
type Identity = string

const (
	IdentityUser Identity = "user"
	IdentityBot  Identity = "bot"
)

// riskOrder maps the Risk taxonomy to a comparable rank. Used by the pruning
// engine's MaxRisk check: c.Risk <= MaxRisk holds when riskOrder[c.Risk] <=
// riskOrder[MaxRisk]. Defined here so the public taxonomy and the comparable
// ordering live next to each other; unknown levels return -1 so callers
// can detect "this is not a recognised risk".
var riskOrder = map[Risk]int{
	RiskRead:          0,
	RiskWrite:         1,
	RiskHighRiskWrite: 2,
}

// RiskRank returns a comparable rank for a Risk value. ok=false when the
// value is not one of the three recognised constants.
func RiskRank(r Risk) (rank int, ok bool) {
	rank, ok = riskOrder[r]
	return rank, ok
}
