package scan

import (
	"database/sql"
	"reflect"
)

// WithRowSkip configures the struct mapper to skip decoding the remaining
// columns of a row when the already-decoded key columns identify a row the
// caller has seen before.
//
// keyColumns are matched against the mapped column names (after any
// [WithStructTagPrefix] is applied by the mapping). For every row, once the
// key columns have been scanned, seen is called with their destinations; if
// it returns true, the remaining mapped columns are neither given
// destinations nor scanned, the [RowValidator] is not consulted, and the
// value built for that row carries only the key columns. Callers are
// expected to substitute their own cached value for such rows — seen
// returning true is a promise that the row's contents are already known.
//
// Skipping never changes which rows are returned, and it degrades to normal
// scanning whenever it cannot apply:
//   - it requires a [TypeConverter] whose destinations implement
//     [sql.Scanner] (the mapper delegates the scan of a non-skipped column
//     to the destination, which is only possible through that interface);
//   - a key column that cannot be resolved disables it for the query;
//   - columns positioned before the last key column in the result set are
//     always decoded (the decision can only be made once the keys are known,
//     and columns are scanned in result-set order).
func WithRowSkip(keyColumns []string, seen func(keyValues []reflect.Value) bool) MappingOption {
	return func(opt *mappingOptions) {
		opt.rowSkipKeys = keyColumns
		opt.rowSkipSeen = seen
	}
}

// rowSkipPlan is built once per query when [WithRowSkip] is configured and
// the key columns resolve to mapped columns that precede at least one other
// mapped column in the result set. Everything in it is reused across rows.
type rowSkipPlan struct {
	seen    func([]reflect.Value) bool
	keyVals []reflect.Value // the current row's key destinations, filled by the before function
	state   rowSkipState
	conds   []condDest

	// set by the mapper that adopts the plan: creates the destination for a
	// filtered column, and the destination slice shared with the after
	// function. Destinations of skippable columns are created lazily, only
	// when the row is not skipped.
	makeDest func(i int) reflect.Value
	scratch  []reflect.Value

	// per filtered-column slots: >= 0 points into keyVals / conds, -1 otherwise
	keySlot  []int
	condSlot []int
}

// skipped reports whether the current row was identified as already known.
func (p *rowSkipPlan) skipped() bool {
	return p.state.decided && p.state.skip
}

// buildRowSkipPlan resolves the key columns against the filtered mapping.
// It returns nil — turning the option into a no-op — when a key column is
// not mapped or when no mapped column comes after the keys in the result
// set.
func buildRowSkipPlan(filtered mapping, keyColumns []string, seen func([]reflect.Value) bool) *rowSkipPlan {
	if len(keyColumns) == 0 || seen == nil {
		return nil
	}

	keySlot := make([]int, len(filtered))
	condSlot := make([]int, len(filtered))
	for i := range filtered {
		keySlot[i], condSlot[i] = -1, -1
	}

	maxKeyCol := -1
	numKeys := 0
	for _, name := range keyColumns {
		found := false
		for i, info := range filtered {
			if info.name != name {
				continue
			}
			found = true
			if keySlot[i] < 0 {
				keySlot[i] = numKeys
				numKeys++
				if info.colIndex > maxKeyCol {
					maxKeyCol = info.colIndex
				}
			}
			break
		}
		if !found {
			return nil
		}
	}

	// columns are scanned in result-set order, so only columns after the
	// last key column can react to the decision; earlier columns are simply
	// always decoded
	numConds := 0
	for i, info := range filtered {
		if keySlot[i] < 0 && info.colIndex > maxKeyCol {
			condSlot[i] = numConds
			numConds++
		}
	}
	if numConds == 0 {
		return nil
	}

	p := &rowSkipPlan{
		seen:     seen,
		keyVals:  make([]reflect.Value, numKeys),
		keySlot:  keySlot,
		condSlot: condSlot,
		conds:    make([]condDest, numConds),
	}
	p.state.decide = func() bool { return p.seen(p.keyVals) }
	ci := 0
	for i := range filtered {
		if condSlot[i] >= 0 {
			p.conds[ci] = condDest{state: &p.state, plan: p, idx: i}
			ci++
		}
	}

	return p
}

// rowSkipState carries the per-row "known row?" decision. It is made lazily
// by the first skippable column that is scanned, so the key columns —
// scanned earlier by result-set order — are already decoded at that point.
type rowSkipState struct {
	decided bool
	skip    bool
	decide  func() bool
}

func (s *rowSkipState) skipRest() bool {
	if !s.decided {
		s.skip = s.decide()
		s.decided = true
	}
	return s.skip
}

// condDest stands in for a skippable column's destination. When the row is
// already known it drops the scan without ever creating the destination;
// otherwise it creates the destination (recording it for the after
// function) and delegates. The instances live in the plan and are reused
// across rows.
type condDest struct {
	state *rowSkipState
	plan  *rowSkipPlan
	idx   int // index into the filtered mapping / destination slice
}

func (c *condDest) Scan(src any) error {
	if c.state.skipRest() {
		return nil
	}

	dest := c.plan.makeDest(c.idx)
	c.plan.scratch[c.idx] = dest

	return dest.Interface().(sql.Scanner).Scan(src)
}
