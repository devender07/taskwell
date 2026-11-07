package workspace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/devender07/taskwell/backend/user/internal/secret"
	"github.com/devender07/taskwell/backend/user/internal/web"
)

const (
	errChooseRole     = "Choose a role."
	errMemberNotFound = "Member not found."
	errInviteNotFound = "Invite not found."
	errInviteInvalid  = "This invite link is invalid or has expired."
	errNoMail         = "Email is not configured. Contact your administrator."
	errUnverified     = "Verify your email before inviting people."
	errInviteLimit    = "Too many invites were sent today. Try again tomorrow."
	errFullWorkspace  = "This workspace is full. Ask an admin to upgrade its plan."
)

// members lists the workspace's members; people who can invite also get its invites. With ?limit= (1 to 100) it's
// one page instead: those matching ?q= by name or email from ?offset=, with has_more and the matching total.
func (h *Handler) members(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	invs := []Invite{}
	if Has(ws.Permissions, permMembersInvite) {
		if invs, err = h.model.invites(ctx, ws.ID); err != nil {
			return err
		}
	}
	query := r.URL.Query()
	if query.Has("limit") {
		limit, err1 := strconv.Atoi(query.Get("limit"))
		offset, err2 := strconv.Atoi(cmp.Or(query.Get("offset"), "0"))
		if err1 != nil || err2 != nil || limit < 1 || limit > 100 || offset < 0 {
			web.Err(w, http.StatusBadRequest, "That page doesn't exist.")
			return nil
		}
		ms, total, err := h.model.memberPage(ctx, ws.ID, userID, strings.TrimSpace(query.Get("q")), offset, limit)
		if err != nil {
			return err
		}
		web.JSON(w, http.StatusOK, map[string]any{"members": ms, "invites": invs, "total": total, "has_more": offset+len(ms) < total})
		return nil
	}
	ms, err := h.model.members(ctx, ws.ID, userID)
	if err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, map[string]any{"members": ms, "invites": invs})
	return nil
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, ok, err := h.access(w, r, userID, permMembersInvite)
	if !ok || err != nil {
		return err
	}
	email := r.FormValue("email")
	if msg := web.CheckEmail(email); msg != "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"email": msg})
		return nil
	}
	role, ok, err := h.assignable(w, r, ws, r.FormValue("role_id"))
	if !ok || err != nil {
		return err
	}
	var projectIDs []string
	if role.Builtin != nil && *role.Builtin == roleGuest {
		if projectIDs, err = h.model.projectIDs(ctx, ws.ID, userID, r.Form["project_keys"]); err != nil {
			return err
		}
		if len(projectIDs) == 0 {
			msg := "Choose at least one project."
			if len(r.Form["project_keys"]) > 0 {
				msg = "Choose projects from this workspace."
			}
			web.Fields(w, http.StatusBadRequest, map[string]string{"project_keys": msg})
			return nil
		}
	}
	email = web.NormEmail(email)
	if in, err := h.model.isMember(ctx, ws.ID, email); err != nil {
		return err
	} else if in {
		web.Fields(w, http.StatusConflict, map[string]string{"email": "They're already in this workspace."})
		return nil
	}
	if full, limit, err := h.model.seatsFull(ctx, ws.ID, email); err != nil {
		return err
	} else if full {
		web.Err(w, http.StatusPaymentRequired, fmt.Sprintf("Your plan allows %d members. Upgrade to invite more.", limit))
		return nil
	}
	if ok, err := h.canSend(w, r, ws, userID); !ok || err != nil {
		return err
	}
	days, err := h.cfg.GetInt(ctx, "invite_ttl_days")
	if err != nil {
		return err
	}
	token := secret.NewToken()
	inv, err := h.model.invite(ctx, ws, email, userID, role.ID, projectIDs, secret.Hash(token), days)
	if err != nil {
		return err
	}
	if err := h.sendInvite(ctx, ws.Name, inv, token, days); err != nil {
		return err
	}
	web.JSON(w, http.StatusCreated, inv)
	return nil
}

func (h *Handler) resendInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ctx := r.Context()
	ws, ok, err := h.access(w, r, userID, permMembersInvite)
	if !ok || err != nil {
		return err
	}
	if ok, err := h.canSend(w, r, ws, userID); !ok || err != nil {
		return err
	}
	days, err := h.cfg.GetInt(ctx, "invite_ttl_days")
	if err != nil {
		return err
	}
	token := secret.NewToken()
	inv, err := h.model.renew(ctx, ws, userID, r.PathValue("id"), secret.Hash(token), days)
	if err != nil {
		return err
	}
	if inv == nil {
		web.Err(w, http.StatusNotFound, errInviteNotFound)
		return nil
	}
	if err := h.sendInvite(ctx, ws.Name, *inv, token, days); err != nil {
		return err
	}
	web.JSON(w, http.StatusOK, inv)
	return nil
}

func (h *Handler) cancelInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permMembersInvite)
	if !ok || err != nil {
		return err
	}
	deleted, err := h.model.cancelInvite(r.Context(), ws, userID, r.PathValue("id"))
	if err != nil {
		return err
	}
	if !deleted {
		web.Err(w, http.StatusNotFound, errInviteNotFound)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// showInvite is public: the invite page needs it before the visitor signs in.
func (h *Handler) showInvite(w http.ResponseWriter, r *http.Request) error {
	info, err := h.model.inviteInfo(r.Context(), secret.Hash(r.PathValue("token")))
	if err != nil {
		return err
	}
	if info == nil {
		web.Err(w, http.StatusNotFound, errInviteInvalid)
		return nil
	}
	web.JSON(w, http.StatusOK, info)
	return nil
}

func (h *Handler) acceptInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, email, err := h.model.accept(r.Context(), userID, secret.Hash(r.PathValue("token")))
	if errors.Is(err, errWrongEmail) {
		web.Err(w, http.StatusForbidden, "This invite is for "+email+". Sign in with that email to accept it.")
		return nil
	}
	if errors.Is(err, errFull) {
		web.Err(w, http.StatusPaymentRequired, errFullWorkspace)
		return nil
	}
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, errInviteInvalid)
		return nil
	}
	web.JSON(w, http.StatusOK, ws)
	return nil
}

// myInvites lists the caller's pending invites to other workspaces, for the invite prompt.
func (h *Handler) myInvites(w http.ResponseWriter, r *http.Request, userID string) error {
	invs, err := h.model.myInvites(r.Context(), userID)
	if err != nil {
		return err
	}
	if invs == nil {
		invs = []PendingInvite{}
	}
	web.JSON(w, http.StatusOK, invs)
	return nil
}

func (h *Handler) acceptMyInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.model.acceptByID(r.Context(), userID, r.PathValue("id"))
	if errors.Is(err, errFull) {
		web.Err(w, http.StatusPaymentRequired, errFullWorkspace)
		return nil
	}
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, errInviteInvalid)
		return nil
	}
	web.JSON(w, http.StatusOK, ws)
	return nil
}

// dismissMyInvite is the prompt's "Not now": the invite stays as a notification in the form's workspace, the one
// the caller is in.
func (h *Handler) dismissMyInvite(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, err := h.model.membership(r.Context(), userID, normSlug(r.FormValue("workspace")))
	if err != nil {
		return err
	}
	if ws == nil {
		web.Err(w, http.StatusNotFound, errNotFound)
		return nil
	}
	found, err := h.model.dismiss(r.Context(), userID, r.PathValue("id"), ws)
	if err != nil {
		return err
	}
	if !found {
		web.Err(w, http.StatusNotFound, errInviteInvalid)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// assignable returns the workspace's role with this id ("" is Member) when the caller may give it to someone.
// Otherwise it writes a 400 (no such role) or 403 (escalation) and returns ok=false.
func (h *Handler) assignable(w http.ResponseWriter, r *http.Request, ws *Workspace, id string) (*Role, bool, error) {
	role, err := h.model.role(r.Context(), ws.ID, id)
	switch {
	case err != nil:
		return nil, false, err
	case role == nil:
		web.Fields(w, http.StatusBadRequest, map[string]string{"role_id": errChooseRole})
		return nil, false, nil
	case !subset(role.Permissions, ws.Permissions):
		web.Err(w, http.StatusForbidden, errEscalation)
		return nil, false, nil
	}
	return role, true, nil
}

// manageable writes a 404 (not a member) or 403 unless the caller holds every permission of the target's current
// role, so nobody demotes or removes someone with more access than they have.
func (h *Handler) manageable(w http.ResponseWriter, r *http.Request, ws *Workspace, userID string) (bool, error) {
	target := r.PathValue("userId")
	if target == userID {
		return true, nil
	}
	role, err := h.model.memberRole(r.Context(), ws.ID, target)
	switch {
	case err != nil:
		return false, err
	case role == nil:
		web.Err(w, http.StatusNotFound, errMemberNotFound)
		return false, nil
	case !subset(role.Permissions, ws.Permissions):
		web.Err(w, http.StatusForbidden, errForbidden)
		return false, nil
	}
	return true, nil
}

func (h *Handler) changeMember(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, permMembersManage)
	if !ok || err != nil {
		return err
	}
	id := strings.TrimSpace(r.FormValue("role_id"))
	if id == "" {
		web.Fields(w, http.StatusBadRequest, map[string]string{"role_id": errChooseRole})
		return nil
	}
	if ok, err := h.manageable(w, r, ws, userID); !ok || err != nil {
		return err
	}
	role, ok, err := h.assignable(w, r, ws, id)
	if !ok || err != nil {
		return err
	}
	mb, err := h.model.setMemberRole(r.Context(), ws, userID, r.PathValue("userId"), role.ID)
	if errors.Is(err, errLastAdmin) {
		web.Err(w, http.StatusConflict, errNoAdmin)
		return nil
	}
	if err != nil {
		return err
	}
	if mb == nil {
		web.Err(w, http.StatusNotFound, errMemberNotFound)
		return nil
	}
	web.JSON(w, http.StatusOK, mb)
	return nil
}

// removeMember removes someone from the workspace, or lets the caller leave it.
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request, userID string) error {
	ws, ok, err := h.access(w, r, userID, "")
	if !ok || err != nil {
		return err
	}
	target := r.PathValue("userId")
	if target != userID {
		if !Has(ws.Permissions, permMembersManage) {
			web.Err(w, http.StatusForbidden, errForbidden)
			return nil
		}
		if ok, err := h.manageable(w, r, ws, userID); !ok || err != nil {
			return err
		}
	}
	removed, err := h.model.removeMember(r.Context(), ws, userID, target)
	if errors.Is(err, errLastAdmin) {
		web.Err(w, http.StatusConflict, errNoAdmin)
		return nil
	}
	if err != nil {
		return err
	}
	if !removed {
		web.Err(w, http.StatusNotFound, errMemberNotFound)
		return nil
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// canSend writes a 503 when SMTP isn't configured, a 403 when the caller's email isn't verified, or a 429 when the
// caller or the workspace has used up today's invite emails, and returns false. Otherwise it counts one send.
func (h *Handler) canSend(w http.ResponseWriter, r *http.Request, ws *Workspace, userID string) (bool, error) {
	ctx := r.Context()
	if ok, err := h.mailReady(w, r); !ok || err != nil {
		return false, err
	}
	if ok, err := h.model.verified(ctx, userID); err != nil || !ok {
		if err == nil {
			web.Err(w, http.StatusForbidden, errUnverified)
		}
		return false, err
	}
	perUser, err := h.cfg.GetInt(ctx, "invite_daily_user_limit")
	if err != nil {
		return false, err
	}
	perWorkspace, err := h.cfg.GetInt(ctx, "invite_daily_workspace_limit")
	if err != nil {
		return false, err
	}
	if ok, err := h.model.takeInviteSend(ctx, ws.ID, userID, perUser, perWorkspace); err != nil || !ok {
		if err == nil {
			web.Err(w, http.StatusTooManyRequests, errInviteLimit)
		}
		return false, err
	}
	return true, nil
}

// mailReady writes a 503 and returns false when SMTP isn't configured.
func (h *Handler) mailReady(w http.ResponseWriter, r *http.Request) (bool, error) {
	ok, err := h.mail.Configured(r.Context())
	if err == nil && !ok {
		web.Err(w, http.StatusServiceUnavailable, errNoMail)
	}
	return ok, err
}

func (h *Handler) sendInvite(ctx context.Context, workspace string, inv Invite, token string, days int) error {
	base, err := h.cfg.Get(ctx, "app_base_url")
	if err != nil {
		return err
	}
	link := strings.TrimRight(base, "/") + "/invite?token=" + url.QueryEscape(token)
	// The subject stays fixed: names are free-form, and a subject is what a phishing relay would want to control.
	return h.mail.Enqueue(ctx, h.model.db, inv.Email, "You're invited to a workspace on Taskwell",
		fmt.Sprintf("%s invited you to join %s on Taskwell.\n\nAccept the invite:\n\n%s\n\nThe link expires in %d days.\n",
			inv.InvitedBy, workspace, link, days))
}
