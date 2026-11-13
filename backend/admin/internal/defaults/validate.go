package defaults

import (
	"regexp"
	"slices"
	"strings"

	"github.com/devender07/taskwell/backend/shared/doc"
)

// The user API's rules and messages for the same fields (workspace/validate.go, issue/validate.go,
// template/form.go); keep them in step. Callers pass names trimmed.

var (
	colorRe    = regexp.MustCompile(`^#[0-9a-f]{6}([0-9a-f]{2})?$`)
	categories = []string{"backlog", "unstarted", "started", "completed", "canceled"}
	typeIcons  = []string{"circle", "circle-dashed", "circle-half", "circle-check", "circle-x", "eye", "refresh", "pause",
		"clock", "flag", "star", "bug", "rocket", "alert", "lock", "message"}
)

const (
	errRoleTaken   = "A role with this name already exists."
	errLabelTaken  = "A label with this name already exists."
	errStatusTaken = "A status with that name already exists."
	errTypeTaken   = "That type already exists."
	errPermission  = "Choose permissions from the list."
	errGuestPerm   = "Guests can't have workspace, member or analytics permissions."
	errChooseType  = "Choose a type from the list."
	errChooseStat  = "Choose a status from the list."
	errChooseLabel = "Choose labels from the list."
	errBadOrderMsg = "Send every status once."
)

// length checks a name of at most max characters (UTF-16 units, like JS).
func length(s, empty, long string, max int) string {
	switch {
	case s == "":
		return empty
	case doc.Len(s) > max:
		return long
	}
	return ""
}

func checkRoleName(s string) string {
	return length(s, "Role name is required.", "Role name must be 40 characters or fewer.", 40)
}

func checkStatusName(s string) string {
	return length(s, "Status name is required.", "Status name must be 30 characters or fewer.", 30)
}

func checkTypeName(s string) string {
	return length(s, "Type name is required.", "Type name must be 30 characters or fewer.", 30)
}

func checkLabelName(s string) string {
	return length(s, "Label name is required.", "Label name must be 30 characters or fewer.", 30)
}

func checkTemplateName(s string) string {
	return length(s, "Enter a name.", "Use 60 characters or fewer.", 60)
}

func checkCategory(s string) string {
	if !slices.Contains(categories, s) {
		return "Choose a category."
	}
	return ""
}

func checkTypeIcon(s string) string {
	if !slices.Contains(typeIcons, s) {
		return "Choose an icon."
	}
	return ""
}

// checkColor takes the color normalized by normColor.
func checkColor(s string) string {
	if !colorRe.MatchString(s) {
		return "Choose a color."
	}
	return ""
}

func normColor(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
