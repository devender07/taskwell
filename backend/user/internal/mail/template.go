package mail

import (
	"bytes"
	"fmt"
	"html/template"
)

// Email is the content of a branded HTML email (Render). Every field but Heading is optional.
type Email struct {
	Preview           string // the inbox preview line after the subject
	Heading           string
	Intro             string
	Text              string // plain text shown as written, line breaks kept (Enqueue's plain-text emails)
	Code              string // a one-time code, shown large
	Card              *Card  // e.g. the issue an email is about
	Button, URL       string // the call to action
	Note              string // small print under the content, e.g. "If you didn't ask for it, ignore this email."
	Footer, FooterURL string // a link in the footer, e.g. email settings
}

// Card is a boxed item: a small label above a title, and a line under it.
type Card struct{ Label, Title, Meta string }

// The light theme (DESIGN-LIGHT.md), inline and table-based so it holds up in Gmail, Outlook and Apple Mail.
var layout = template.Must(template.New("email").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<meta name="supported-color-schemes" content="light">
<title>{{.Heading}}</title>
</head>
<body style="margin:0;padding:0;background:#F5F5F7;">
{{if .Preview}}<div style="display:none;max-height:0;overflow:hidden;opacity:0;">{{.Preview}}</div>{{end}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#F5F5F7;">
<tr><td align="center" style="padding:40px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:560px;font-family:'Plus Jakarta Sans',-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">
<tr><td style="padding:0 8px 24px;"><table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
<td style="padding-right:8px;"><img src="cid:logo" width="32" height="32" alt="" style="display:block;border:0;border-radius:7px;"></td>
<td style="font-size:18px;line-height:24px;font-weight:590;color:#1D1D1F;letter-spacing:-0.2px;">Taskwell</td>
</tr></table></td></tr>
<tr><td style="background:#FFFFFF;border:1px solid #E0E0E0;border-radius:8px;padding:40px 32px;">
<h1 style="margin:0 0 12px;font-size:24px;line-height:32px;font-weight:590;color:#1D1D1F;">{{.Heading}}</h1>
{{if .Intro}}<p style="margin:0 0 24px;font-size:15px;line-height:24px;color:#1D1D1F;">{{.Intro}}</p>{{end}}
{{if .Text}}<div style="font-size:15px;line-height:24px;color:#1D1D1F;white-space:pre-wrap;word-break:break-word;">{{.Text}}</div>{{end}}
{{if .Code}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
<tr><td align="center" style="background:#F5F5F7;border:1px solid #E0E0E0;border-radius:8px;padding:24px 16px;font-family:'Berkeley Mono',SFMono-Regular,Menlo,Consolas,monospace;font-size:32px;line-height:40px;font-weight:590;letter-spacing:8px;color:#1D1D1F;">{{.Code}}</td></tr>
</table>{{end}}
{{with .Card}}<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
<tr><td style="background:#F5F5F7;border:1px solid #E0E0E0;border-radius:8px;padding:16px 20px;">
{{if .Label}}<div style="font-family:'Berkeley Mono',SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;line-height:20px;color:#6E6E73;">{{.Label}}</div>{{end}}
<div style="font-size:16px;line-height:24px;font-weight:590;color:#1D1D1F;">{{.Title}}</div>
{{if .Meta}}<div style="margin-top:4px;font-size:13px;line-height:20px;color:#6E6E73;">{{.Meta}}</div>{{end}}
</td></tr>
</table>{{end}}
{{if .URL}}<table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 24px;">
<tr><td style="border-radius:9999px;background:#0066CC;">
<a href="{{.URL}}" style="display:inline-block;padding:12px 24px;font-size:15px;line-height:20px;font-weight:590;color:#FFFFFF;text-decoration:none;border-radius:9999px;">{{.Button}}</a>
</td></tr>
</table>
<p style="margin:0;font-size:13px;line-height:20px;color:#6E6E73;">Or open this link: <a href="{{.URL}}" style="color:#0066CC;word-break:break-all;">{{.URL}}</a></p>{{end}}
{{if .Note}}<p style="margin:{{if .URL}}24px{{else}}0{{end}} 0 0;font-size:13px;line-height:20px;color:#6E6E73;">{{.Note}}</p>{{end}}
</td></tr>
<tr><td style="padding:24px 8px 0;font-size:12px;line-height:18px;color:#6E6E73;">
Taskwell, project management for SaaS teams.{{if .FooterURL}} <a href="{{.FooterURL}}" style="color:#6E6E73;text-decoration:underline;">{{.Footer}}</a>{{end}}
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>
`))

// Render returns e as the HTML part of an email. Every value is escaped; a URL that isn't http(s) is neutralized.
func Render(e Email) (string, error) {
	var b bytes.Buffer
	if err := layout.Execute(&b, e); err != nil {
		return "", fmt.Errorf("render email: %w", err)
	}
	return b.String(), nil
}
