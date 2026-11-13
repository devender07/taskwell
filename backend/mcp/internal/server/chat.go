package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// chatTools registers the tools for chat, the inbox and invites.
func (t *tools) chatTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "list_chats", Annotations: readOnly,
		Description: "List your chats in a workspace (the workspace chat, groups and direct messages) with unread counts."}, t.listChats)
	mcp.AddTool(s, &mcp.Tool{Name: "read_chat", Annotations: readOnly,
		Description: "Read a chat's latest messages, oldest first. When has_more, pass before to get older ones."}, t.readChat)
	mcp.AddTool(s, &mcp.Tool{Name: "send_message",
		Description: "Post a message in a chat, or reply in a thread with parent_id (a message id from read_chat)."}, t.sendMessage)
	mcp.AddTool(s, &mcp.Tool{Name: "send_direct_message",
		Description: "Send a direct message to a workspace member, by email or name."}, t.sendDirectMessage)
	mcp.AddTool(s, &mcp.Tool{Name: "list_notifications", Annotations: readOnly,
		Description: "Your notifications in a workspace, newest first. When has_more, pass before to get older ones."}, t.listNotifications)
	mcp.AddTool(s, &mcp.Tool{Name: "mark_notifications_read",
		Description: "Mark notifications read: the ids given, or all of them with all true."}, t.markNotificationsRead)
	mcp.AddTool(s, &mcp.Tool{Name: "invite_member",
		Description: "Invite someone to the workspace by email with a role (a role name such as Member). They get an email to join."},
		t.inviteMember)
}

type chatIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ChatID    string `json:"chat_id" jsonschema:"from list_chats"`
	Before    string `json:"before,omitempty" jsonschema:"the before value of the previous page"`
}

type messageIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	ChatID    string `json:"chat_id" jsonschema:"from list_chats"`
	Body      string `json:"body" jsonschema:"plain text; blank lines separate paragraphs"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"reply in this message's thread"`
}

type directIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Member    string `json:"member" jsonschema:"member's email or name"`
	Body      string `json:"body" jsonschema:"plain text; blank lines separate paragraphs"`
}

type pageIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Before    string `json:"before,omitempty" jsonschema:"the before value of the previous page"`
}

type markReadIn struct {
	Workspace string   `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	IDs       []string `json:"ids,omitempty" jsonschema:"notification ids, from list_notifications"`
	All       bool     `json:"all,omitempty" jsonschema:"mark every notification read"`
}

type inviteIn struct {
	Workspace string `json:"workspace" jsonschema:"workspace slug, from list_workspaces"`
	Email     string `json:"email"`
	Role      string `json:"role" jsonschema:"role name, such as Member or Admin"`
}

type message struct {
	ID         string          `json:"id"`
	ParentID   *string         `json:"parent_id"`
	Author     *person         `json:"author"`
	Body       json.RawMessage `json:"body"`
	Deleted    bool            `json:"deleted"`
	CreatedAt  string          `json:"created_at"`
	ReplyCount int             `json:"reply_count"`
}

type messageOut struct {
	ID         string `json:"id"`
	ParentID   string `json:"parent_id,omitempty"`
	Author     string `json:"author,omitempty"`
	Text       string `json:"text,omitempty"`
	Deleted    bool   `json:"deleted,omitempty"`
	CreatedAt  string `json:"created_at"`
	ReplyCount int    `json:"reply_count,omitempty"`
}

func msgOut(m message) messageOut {
	o := messageOut{ID: m.ID, Author: name(m.Author), Text: plainText(m.Body), Deleted: m.Deleted, CreatedAt: m.CreatedAt,
		ReplyCount: m.ReplyCount}
	if m.ParentID != nil {
		o.ParentID = *m.ParentID
	}
	return o
}

func (t *tools) listChats(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, any, error) {
	var cs []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Unread      int    `json:"unread"`
		Mentioned   bool   `json:"mentioned"`
		LastMessage *struct {
			Author    *person `json:"author"`
			Excerpt   string  `json:"excerpt"`
			CreatedAt string  `json:"created_at"`
		} `json:"last_message"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "chats"), nil, &cs); err != nil {
		return nil, nil, err
	}
	type chatOut struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Unread    int    `json:"unread,omitempty"`
		Mentioned bool   `json:"mentioned,omitempty"`
		Last      string `json:"last_message,omitempty"` // "Author: excerpt"
		LastAt    string `json:"last_message_at,omitempty"`
	}
	o := make([]chatOut, len(cs))
	for k, c := range cs {
		o[k] = chatOut{ID: c.ID, Kind: c.Kind, Name: c.Name, Unread: c.Unread, Mentioned: c.Mentioned}
		if m := c.LastMessage; m != nil {
			o[k].Last, o[k].LastAt = name(m.Author)+": "+m.Excerpt, m.CreatedAt
		}
	}
	return result(o)
}

func (t *tools) readChat(ctx context.Context, _ *mcp.CallToolRequest, in chatIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Messages []message `json:"messages"`
		HasMore  bool      `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "chats", in.ChatID, "messages"), before(in.Before), &res); err != nil {
		return nil, nil, err
	}
	o := struct {
		Messages []messageOut `json:"messages"`
		HasMore  bool         `json:"has_more"`
		Before   string       `json:"before,omitempty"`
	}{Messages: make([]messageOut, len(res.Messages)), HasMore: res.HasMore}
	for k, m := range res.Messages {
		o.Messages[k] = msgOut(m)
	}
	if res.HasMore && len(res.Messages) > 0 {
		o.Before = res.Messages[0].ID // oldest first
	}
	return result(o)
}

func (t *tools) sendMessage(ctx context.Context, _ *mcp.CallToolRequest, in messageIn) (*mcp.CallToolResult, any, error) {
	return t.send(ctx, in.Workspace, in.ChatID, in.Body, in.ParentID)
}

func (t *tools) send(ctx context.Context, slug, chatID, body, parentID string) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(body) == "" {
		return nil, nil, errors.New("Write a message.")
	}
	form := url.Values{"body": {toDoc(body)}}
	if parentID = strings.TrimSpace(parentID); parentID != "" {
		form.Set("parent_id", parentID)
	}
	var m message
	if err := t.api.post(ctx, ws(slug, "chats", chatID, "messages"), form, &m); err != nil {
		return nil, nil, err
	}
	return result(struct {
		ChatID string `json:"chat_id"`
		messageOut
	}{chatID, msgOut(m)})
}

func (t *tools) sendDirectMessage(ctx context.Context, _ *mcp.CallToolRequest, in directIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Body) == "" {
		return nil, nil, errors.New("Write a message.")
	}
	m, err := t.member(ctx, in.Workspace, in.Member)
	if err != nil {
		return nil, nil, err
	}
	var c struct {
		ID string `json:"id"`
	}
	if err := t.api.post(ctx, ws(in.Workspace, "chats", "direct"), url.Values{"user_id": {m.ID}}, &c); err != nil {
		return nil, nil, err
	}
	return t.send(ctx, in.Workspace, c.ID, in.Body, "")
}

func (t *tools) listNotifications(ctx context.Context, _ *mcp.CallToolRequest, in pageIn) (*mcp.CallToolResult, any, error) {
	var res struct {
		Notifications []struct {
			ID    string  `json:"id"`
			Kind  string  `json:"kind"`
			Actor *person `json:"actor"`
			Issue *struct {
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"issue"`
			Chat *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"chat"`
			Data      json.RawMessage `json:"data"`
			Read      bool            `json:"read"`
			CreatedAt string          `json:"created_at"`
		} `json:"notifications"`
		HasMore bool `json:"has_more"`
	}
	if err := t.api.get(ctx, ws(in.Workspace, "notifications"), before(in.Before), &res); err != nil {
		return nil, nil, err
	}
	type noteOut struct {
		ID         string          `json:"id"`
		Kind       string          `json:"kind"`
		Actor      string          `json:"actor,omitempty"`
		Issue      string          `json:"issue,omitempty"`
		IssueTitle string          `json:"issue_title,omitempty"`
		Chat       string          `json:"chat,omitempty"`
		ChatID     string          `json:"chat_id,omitempty"`
		Data       json.RawMessage `json:"data,omitempty"`
		Read       bool            `json:"read"`
		CreatedAt  string          `json:"created_at"`
	}
	o := struct {
		Notifications []noteOut `json:"notifications"`
		HasMore       bool      `json:"has_more"`
		Before        string    `json:"before,omitempty"`
	}{Notifications: make([]noteOut, len(res.Notifications)), HasMore: res.HasMore}
	for k, n := range res.Notifications {
		no := noteOut{ID: n.ID, Kind: n.Kind, Actor: name(n.Actor), Data: n.Data, Read: n.Read, CreatedAt: n.CreatedAt}
		if string(n.Data) == "null" || string(n.Data) == "{}" {
			no.Data = nil
		}
		if n.Issue != nil {
			no.Issue, no.IssueTitle = n.Issue.Identifier, n.Issue.Title
		}
		if n.Chat != nil {
			no.Chat, no.ChatID = n.Chat.Name, n.Chat.ID
		}
		o.Notifications[k] = no
	}
	if res.HasMore && len(o.Notifications) > 0 {
		o.Before = o.Notifications[len(o.Notifications)-1].ID
	}
	return result(o)
}

func (t *tools) markNotificationsRead(ctx context.Context, _ *mcp.CallToolRequest, in markReadIn) (*mcp.CallToolResult, any, error) {
	form := url.Values{"ids": in.IDs}
	if in.All {
		form.Set("all", "true")
	}
	if err := t.api.post(ctx, ws(in.Workspace, "notifications", "read"), form, nil); err != nil {
		return nil, nil, err
	}
	return result(map[string]bool{"done": true})
}

func (t *tools) inviteMember(ctx context.Context, _ *mcp.CallToolRequest, in inviteIn) (*mcp.CallToolResult, any, error) {
	var rs []label // a role is an id and a name too
	if err := t.api.get(ctx, ws(in.Workspace, "roles"), nil, &rs); err != nil {
		return nil, nil, err
	}
	r, err := find("role", in.Role, rs, func(r label) []string { return []string{r.Name} }, labelName)
	if err != nil {
		return nil, nil, err
	}
	var inv struct {
		Email     string `json:"email"`
		ExpiresAt string `json:"expires_at"`
		Role      struct {
			Name string `json:"name"`
		} `json:"role"`
	}
	if err := t.api.post(ctx, ws(in.Workspace, "invites"), url.Values{"email": {strings.TrimSpace(in.Email)}, "role_id": {r.ID}}, &inv); err != nil {
		return nil, nil, err
	}
	return result(struct {
		Email     string `json:"email"`
		Role      string `json:"role"`
		ExpiresAt string `json:"expires_at"`
	}{inv.Email, inv.Role.Name, inv.ExpiresAt})
}
