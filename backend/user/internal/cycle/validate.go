package cycle

import (
	"fmt"
	"regexp"
	"time"

	"github.com/devender07/taskwell/backend/user/internal/doc"
)

const maxDays = 90

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseDate reads a YYYY-MM-DD date; ok=false when it isn't one.
func parseDate(s string) (time.Time, bool) {
	if !dateRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.DateOnly, s)
	return t, err == nil
}

// checkName takes the trimmed custom name; "" is the default name. Lengths count UTF-16 units like the frontend.
func checkName(s string) string {
	if doc.Len(s) > 60 {
		return "Use 60 characters or fewer."
	}
	return ""
}

// checkSpan is the ends_on message for a cycle from starts to ends, both days included.
func checkSpan(starts, ends time.Time) string {
	switch {
	case ends.Before(starts):
		return "The end date can't be before the start date."
	case ends.Sub(starts) >= maxDays*24*time.Hour:
		return fmt.Sprintf("A cycle can be at most %d days.", maxDays)
	}
	return ""
}

// overlapMsg is the starts_on message for a clash with the named cycle.
func overlapMsg(name string, starts, ends, today time.Time) string {
	return fmt.Sprintf("This overlaps %s (%s – %s).", name, shortDate(starts, today), shortDate(ends, today))
}

// shortDate is "Jan 2", with the year when it isn't today's.
func shortDate(t, today time.Time) string {
	if t.Year() == today.Year() {
		return t.Format("Jan 2")
	}
	return t.Format("Jan 2, 2006")
}
