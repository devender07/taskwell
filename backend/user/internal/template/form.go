package template

import (
	"cmp"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

// values are a template's stored fields as the form edits them; "" is none, every 0 doesn't repeat.
type values struct {
	name, title                      string
	description                      json.RawMessage
	projectKey, statusID, assigneeID string
	priority                         int
	labelIDs                         []string
	every                            int
	unit, nextOn                     string // nextOn is YYYY-MM-DD
	paused                           bool
}

var units = []string{"day", "week", "month"}

// Same rules and messages as frontend/shared/src/validate/validate.ts; lengths count UTF-16 units like JS.
const (
	errName     = "Enter a name."
	errNameLen  = "Use 60 characters or fewer."
	errTitleLen = "Title must be 200 characters or fewer."
	errTitle    = "A repeating template needs a title."
	errProject  = "A repeating template needs a project."
	errEvery    = "Repeat every 1 to 365."
	errUnit     = "Choose day, week or month."
	errStartsOn = "Pick today or a later date."
	errPriority = "Choose a priority."
	errStatus   = "Choose a status."
	errAssignee = "Choose someone in this workspace."
	errLabels   = "Choose labels from this workspace."
)

// apply sets v's fields that the form sends and returns the field errors of the result ("" when valid), today being
// the UTC date. Sending repeat_every, repeat_unit and starts_on all empty stops the repeat. A repeat left due in the
// past (a template unpaused after a while) moves on to its next date from today.
func apply(v *values, form url.Values, today time.Time) map[string]string {
	errs := map[string]string{}
	sent := func(k string) (string, bool) {
		s, ok := form[k]
		if !ok {
			return "", false
		}
		return strings.TrimSpace(s[0]), true
	}
	if s, ok := sent("name"); ok {
		v.name = s
	}
	if s, ok := sent("title"); ok {
		v.title = s
	}
	if s, ok := sent("description"); ok {
		v.description, errs["description"] = nil, ""
		if s != "" {
			v.description, errs["description"] = doc.Check([]byte(s), "Description", 50000)
		}
	}
	if s, ok := sent("project"); ok {
		v.projectKey = strings.ToUpper(s)
	}
	if s, ok := sent("status_id"); ok {
		v.statusID = s
	}
	if s, ok := sent("assignee_id"); ok {
		v.assigneeID = s
	}
	if s, ok := sent("priority"); ok {
		n, err := strconv.Atoi(cmp.Or(s, "0"))
		if v.priority = n; err != nil || n < 0 || n > 4 {
			v.priority, errs["priority"] = 0, errPriority
		}
	}
	if ids, ok := form["label_ids"]; ok {
		v.labelIDs = slices.Compact(slices.Sorted(slices.Values(slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == "" }))))
	}
	every, okEvery := sent("repeat_every")
	unit, okUnit := sent("repeat_unit")
	startsOn, okStarts := sent("starts_on")
	switch {
	case !okEvery && !okUnit && !okStarts:
	case every == "" && unit == "" && startsOn == "":
		v.every, v.unit, v.nextOn = 0, "", ""
	default:
		n, err := strconv.Atoi(every)
		if err != nil || n < 1 || n > 365 {
			errs["repeat_every"] = errEvery
		}
		if !slices.Contains(units, unit) {
			errs["repeat_unit"] = errUnit
		}
		if d, err := time.Parse(time.DateOnly, startsOn); err != nil || d.Before(today) {
			errs["starts_on"] = errStartsOn
		}
		v.every, v.unit, v.nextOn = n, unit, startsOn
	}
	if s, ok := sent("paused"); ok {
		v.paused = s == "1"
	}

	switch n := doc.Len(v.name); {
	case n == 0:
		errs["name"] = errName
	case n > 60:
		errs["name"] = errNameLen
	}
	switch {
	case doc.Len(v.title) > 200:
		errs["title"] = errTitleLen
	case v.title == "" && v.every > 0:
		errs["title"] = errTitle
	}
	if v.projectKey == "" && v.every > 0 {
		errs["project"] = errProject
	}
	// Ids that aren't uuids can't be the workspace's; checking here keeps them out of uuid comparisons.
	if v.statusID != "" && !web.IsUUID(v.statusID) {
		errs["status_id"] = errStatus
	}
	if v.assigneeID != "" && !web.IsUUID(v.assigneeID) {
		errs["assignee_id"] = errAssignee
	}
	if slices.ContainsFunc(v.labelIDs, func(s string) bool { return !web.IsUUID(s) }) {
		errs["label_ids"] = errLabels
	}
	if errs["repeat_every"] == "" && errs["repeat_unit"] == "" && errs["starts_on"] == "" && v.every > 0 && !v.paused {
		if d, err := time.Parse(time.DateOnly, v.nextOn); err == nil && d.Before(today) {
			v.nextOn = upcoming(d, v.every, v.unit, today).Format(time.DateOnly)
		}
	}
	return errs
}

// next is the date every units after d (UTC dates). A month step keeps d's day, or the month's last day when it's
// shorter: Jan 31 + 1 month = Feb 28.
// ponytail: only next_on is stored, so a clamped day sticks (Jan 31, Feb 28, Mar 28); store the anchor day if that
// matters.
func next(d time.Time, every int, unit string) time.Time {
	switch unit {
	case "day":
		return d.AddDate(0, 0, every)
	case "week":
		return d.AddDate(0, 0, 7*every)
	}
	first := time.Date(d.Year(), d.Month()+time.Month(every), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(d.Day(), last), 0, 0, 0, 0, time.UTC)
}

// upcoming steps d on by the interval until it's after today, so missed runs make one issue, not a burst.
func upcoming(d time.Time, every int, unit string, today time.Time) time.Time {
	for !d.After(today) {
		d = next(d, every, unit)
	}
	return d
}

// utcToday is today's date in UTC, at midnight.
func utcToday() time.Time {
	y, m, d := time.Now().UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
