// Package tagger provides utilities to automatically suggest tags for a
// financial transaction based on a set of TagRule patterns.
//
// Rules are matched by case-insensitive substring search against the
// transaction Name and Memo fields. Family rules take priority over system
// rules: if a family rule matches the same pattern, its tags replace the
// system tags for that pattern.
package tagger

import (
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/willGabrielPereira/finager-backend/internal/models"
)

// Apply receives an ordered slice of TagRules (system rules first, then family
// rules) and returns the deduplicated set of tag ObjectIDs that match the
// given name and memo strings.
//
// Family rules OVERRIDE system rules for the same pattern: if both a system
// rule and a family rule match "amazon", the family rule tags win entirely for
// that pattern (rather than merging), which lets a family point amazon→
// [entretID] without also getting [comprasID] from the system rule.
func Apply(rules []models.TagRule, name, memo string) []bson.ObjectID {
	nameLow := strings.ToLower(name)
	memoLow := strings.ToLower(memo)

	// Two-pass: collect system matches then let family rules override per pattern.
	// patternTags accumulates the final result keyed by canonical pattern string.
	type entry struct {
		tagIDs   []bson.ObjectID
		isFamily bool
	}
	patternMap := make(map[string]entry)

	for _, r := range rules {
		pat := strings.ToLower(r.Pattern)
		if pat == "" {
			continue
		}
		if !strings.Contains(nameLow, pat) && !strings.Contains(memoLow, pat) {
			continue
		}

		existing, seen := patternMap[pat]
		if !seen {
			patternMap[pat] = entry{tagIDs: r.Tags, isFamily: !r.IsSystem}
			continue
		}

		// Family rule always wins over system rule for the same pattern.
		if !r.IsSystem && !existing.isFamily {
			patternMap[pat] = entry{tagIDs: r.Tags, isFamily: true}
		}
	}

	// Flatten and deduplicate all matched tag IDs.
	seen := make(map[bson.ObjectID]struct{})
	var result []bson.ObjectID
	for _, e := range patternMap {
		for _, id := range e.tagIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				result = append(result, id)
			}
		}
	}

	return result
}
