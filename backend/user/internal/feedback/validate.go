package feedback

import (
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/doc"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	maxBodyLen = 5000
	maxNameLen = 80
)

// input is a feedback's fields, trimmed; Email and Name are nil when empty.
type input struct {
	Body  string
	Email *string
	Name  *string
}

// read takes the form's body, email and name and returns them with their messages; the email is required on the
// public form.
func read(get func(string) string, emailRequired bool) (input, map[string]string) {
	body := strings.TrimSpace(get("body"))
	email := web.NormEmail(get("email"))
	name := strings.TrimSpace(get("name"))
	errs := map[string]string{"body": checkBody(body), "email": checkEmail(email, emailRequired), "name": checkName(name)}
	return input{Body: body, Email: optional(email), Name: optional(name)}, errs
}

func checkBody(s string) string {
	switch {
	case s == "":
		return "Write the feedback."
	case doc.Len(s) > maxBodyLen:
		return "Feedback must be 5,000 characters or fewer."
	}
	return ""
}

func checkEmail(s string, required bool) string {
	switch {
	case s == "" && required:
		return "Enter your email."
	case s != "" && web.CheckEmail(s) != "":
		return "Enter a valid email."
	}
	return ""
}

func checkName(s string) string {
	if doc.Len(s) > maxNameLen {
		return "Name must be 80 characters or fewer."
	}
	return ""
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
