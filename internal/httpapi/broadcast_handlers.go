package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/amarseillaise/3x-ui-cm/internal/push"
	"github.com/amarseillaise/3x-ui-cm/internal/store"
)

type broadcastRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Target struct {
		All    bool     `json:"all"`
		SubIDs []string `json:"subIds"`
		Emails []string `json:"emails"`
	} `json:"target"`
}

func validPushURL(u string) bool {
	return u == "" || strings.HasPrefix(u, "/") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// handleAdminPush is POST /api/admin/push: a custom notification to all
// clients, to specific subscription ids or to clients resolved by email.
func (s *Server) handleAdminPush(w http.ResponseWriter, r *http.Request) {
	if !s.push.Enabled() {
		writeError(w, http.StatusConflict, "push_disabled", "Уведомления не настроены на сервере (VAPID)")
		return
	}
	var req broadcastRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректное тело запроса")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	req.URL = strings.TrimSpace(req.URL)
	switch {
	case req.Title == "" || utf8.RuneCountInString(req.Title) > 100:
		writeError(w, http.StatusBadRequest, "bad_request", "Заголовок: от 1 до 100 символов")
		return
	case utf8.RuneCountInString(req.Body) > 500:
		writeError(w, http.StatusBadRequest, "bad_request", "Текст: не больше 500 символов")
		return
	case !validPushURL(req.URL):
		writeError(w, http.StatusBadRequest, "bad_request", "Ссылка должна начинаться с / или http(s)://")
		return
	}
	targets := 0
	if req.Target.All {
		targets++
	}
	if len(req.Target.SubIDs) > 0 {
		targets++
	}
	if len(req.Target.Emails) > 0 {
		targets++
	}
	if targets != 1 {
		writeError(w, http.StatusBadRequest, "bad_request", "Укажите ровно одну цель: all, subIds или emails")
		return
	}

	ctx := r.Context()
	var subIDs []string // nil = everyone
	var unknown []string
	switch {
	case len(req.Target.SubIDs) > 0:
		subIDs = req.Target.SubIDs
	case len(req.Target.Emails) > 0:
		list, err := s.xui.ListClients(ctx)
		if err != nil {
			s.log.Warn("broadcast: panel unavailable", "err", err)
			writeError(w, http.StatusBadGateway, "panel_unavailable", "Панель временно недоступна")
			return
		}
		byEmail := make(map[string]string, len(list))
		for _, c := range list {
			byEmail[c.Email] = c.SubID
		}
		subIDs = []string{}
		for _, e := range req.Target.Emails {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			if id, ok := byEmail[e]; ok && id != "" {
				subIDs = append(subIDs, id)
			} else {
				unknown = append(unknown, e)
			}
		}
		if len(subIDs) == 0 {
			writeError(w, http.StatusNotFound, "client_not_found", "Ни один email не найден в панели")
			return
		}
	}

	subs, err := s.store.ListPushSubscriptions(ctx, store.RoleClient, subIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "Не удалось загрузить push-подписки")
		return
	}
	res := s.push.SendTo(ctx, subs, push.Message{Title: req.Title, Body: req.Body, URL: req.URL, Tag: "custom"})
	now := s.now().UnixMilli()
	nonce := make([]byte, 4)
	_, _ = rand.Read(nonce)
	ref := strconv.FormatInt(now, 10) + "-" + hex.EncodeToString(nonce) // broadcasts are never deduplicated
	if _, err := s.store.RecordNotification(ctx, store.Notification{Kind: "custom", Ref: ref, Title: req.Title, SentAt: now}); err != nil {
		s.log.Error("broadcast: log", "err", err)
	}
	s.log.Info("broadcast", "recipients", len(subs), "sent", res.Sent, "failed", res.Failed, "removed", res.Removed)
	if unknown == nil {
		unknown = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"recipients":    len(subs),
		"sent":          res.Sent,
		"failed":        res.Failed,
		"removed":       res.Removed,
		"unknownEmails": unknown,
	})
}
