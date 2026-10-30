// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/QiyanAI/derive-world/internal/derive"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/publication"
	"github.com/QiyanAI/derive-world/internal/social"
	"github.com/QiyanAI/derive-world/internal/world"
)

func (a *App) worldServiceUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if a.world != nil && a.objects != nil {
		return false
	}
	httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
	return true
}

// worldStage returns the visible object stage and real derivation edges.
func (a *App) worldStage(w http.ResponseWriter, r *http.Request) {
	if a.worldServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	stage, err := a.world.Stage(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stage)
}

// worldLayout returns the caller's private spatial positions. Another user's
// layout is never visible and layouts never affect object state.
func (a *App) worldLayout(w http.ResponseWriter, r *http.Request) {
	if a.worldServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	layout, err := a.world.Layout(r.Context(), principal.User.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, layout)
}

// saveWorldLayout upserts one node position. The object must remain visible to
// the caller; the position is view state only.
func (a *App) saveWorldLayout(w http.ResponseWriter, r *http.Request) {
	if a.worldServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if a.rejectActorOverrideInBody(w, r) {
		return
	}
	var input struct {
		ObjectID string  `json:"object_id"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	objectID, err := uuid.Parse(input.ObjectID)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "object_id is invalid"))
		return
	}
	if math.IsNaN(input.X) || math.IsNaN(input.Y) ||
		math.IsInf(input.X, 0) || math.IsInf(input.Y, 0) ||
		input.X < -10000 || input.X > 10000 || input.Y < -10000 || input.Y > 10000 {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "position is out of bounds"))
		return
	}
	item, err := a.world.SaveLayout(r.Context(), principal.User.ID, objectID, input.X, input.Y)
	if errors.Is(err, world.ErrLayoutObjectNotFound) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

// pulse merges real object changes with real social and publication signals.
func (a *App) pulse(w http.ResponseWriter, r *http.Request) {
	if a.worldServiceUnavailable(w, r) {
		return
	}
	principal := principalFrom(r.Context())
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 30, 50)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	items, err := a.world.PulseEvents(r.Context(), principal.User.ID, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var activity *social.Activity
	if a.social != nil {
		activity, err = a.social.RecentActivity(r.Context(), principal.User.ID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
	}
	consents := make([]publication.Consent, 0)
	if a.publication != nil {
		consents, err = a.publication.PendingConsents(r.Context(), principal.User.ID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items       []world.PulseItem     `json:"items"`
		Social      *social.Activity      `json:"social"`
		Consents    []publication.Consent `json:"publication_consents"`
		GeneratedAt time.Time             `json:"generated_at"`
	}{
		Items: items, Social: activity, Consents: consents, GeneratedAt: time.Now().UTC(),
	})
}

// getLens returns the authorized source, history, branch, and inheritance view.
func (a *App) getLens(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	object, actorID, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	principal := principalFrom(r.Context())
	var (
		lineage     *objects.DeriveLineage
		err         error
		publicChild = false
	)
	if a.publication != nil {
		var source *objects.PublicSource
		source, err = a.publication.PublicSourceForChild(r.Context(), object.ID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
		publicChild = source != nil
		if source != nil && source.Active {
			lineage, err = a.publication.PublicLineageForChild(r.Context(), object.ID)
			if err != nil {
				a.writeDomainError(w, r, err)
				return
			}
		}
	}
	if !publicChild {
		lineage, err = a.objects.LineageForObject(r.Context(), principal.User.ID, object.ID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
	}
	versions, err := a.objects.Versions(r.Context(), principal.User.ID, object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	children, err := a.objects.Children(r.Context(), principal.User.ID, object.ID)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	proposals := make([]objects.Proposal, 0)
	if objects.HasPermission(object.Permissions, objects.PermContribute) {
		proposals, err = a.objects.ListProposals(r.Context(), *actorID, object.ID)
		if err != nil {
			a.writeDomainError(w, r, err)
			return
		}
	}
	fields := derive.AvailableFields(object.ObjectType)
	if fields == nil {
		fields = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Object            objects.ObjectView     `json:"object"`
		Lineage           *objects.DeriveLineage `json:"lineage"`
		Versions          []objects.LensVersion  `json:"versions"`
		Children          []objects.BranchChild  `json:"children"`
		InheritanceFields []string               `json:"inheritance_fields"`
		Proposals         []objects.Proposal     `json:"proposals"`
	}{
		Object: object.View(), Lineage: lineage, Versions: versions,
		Children: children, InheritanceFields: fields, Proposals: proposals,
	})
}

// getObjectVersion returns one authorized historical snapshot.
func (a *App) getObjectVersion(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "version is invalid"))
		return
	}
	currentVersion, objectType, state, err := a.objects.StateAtVersionForActor(
		r.Context(), principalFrom(r.Context()).User.ID, object.ID, version)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		ObjectID       string          `json:"object_id"`
		ObjectType     string          `json:"object_type"`
		Version        int             `json:"version"`
		CurrentVersion int             `json:"current_version"`
		State          json.RawMessage `json:"state"`
	}{
		ObjectID: object.ID.String(), ObjectType: objectType,
		Version: version, CurrentVersion: currentVersion, State: state,
	})
}

func (a *App) listDiscussions(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) || a.social == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	limit, err := httpx.ParseLimit(r.URL.Query().Get("limit"), 30, 100)
	if err != nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "limit is invalid"))
		return
	}
	var cursor int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			httpx.WriteError(w, r, httpx.NewError(httpx.CodeInvalidRequest, "cursor is invalid"))
			return
		}
	}
	items, next, err := a.social.ListDiscussions(r.Context(), object.ID, cursor, limit)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	var nextCursor *string
	if next != nil {
		value := strconv.FormatInt(*next, 10)
		nextCursor = &value
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items      []social.Discussion `json:"items"`
		NextCursor *string             `json:"next_cursor"`
	}{Items: items, NextCursor: nextCursor})
}

func (a *App) postDiscussion(w http.ResponseWriter, r *http.Request) {
	if a.objectServiceUnavailable(w, r) || a.social == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	if !a.checkCSRF(w, r, principal) {
		return
	}
	if !a.allowUserLimit(w, r, principal.User.ID, a.socialWriteLimiter, 60,
		"social_rate_limited", "too many social writes") {
		return
	}
	object, _, ok := a.loadAuthorized(w, r, objects.PermRead)
	if !ok {
		return
	}
	body, err := a.readRequestBody(w, r)
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	if field := detectActorOverride(r, body); field != "" {
		a.rejectActorOverride(w, r, &principal.User.ID, field)
		return
	}
	var input struct {
		Body            string `json:"body"`
		ClientMessageID string `json:"client_message_id"`
	}
	if err := httpx.DecodeJSON(w, r, &input, a.config.MaxRequestBytes); err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	discussion, err := a.social.PostDiscussion(r.Context(), object.ID, principal.User.ID,
		input.Body, input.ClientMessageID, httpx.RequestIDFromContext(r.Context()),
		httpx.HashIPBytes(a.config.IPHashPepper, httpx.ClientIP(r, a.config.TrustProxyHeaders)))
	if err != nil {
		a.writeDomainError(w, r, err)
		return
	}
	a.notifyObjectMembers(r.Context(), object.ID)
	httpx.WriteJSON(w, http.StatusCreated, discussion)
}

// systemStatus exposes non-secret model, quota, network, and readiness facts.
func (a *App) systemStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	_ = principal
	databaseOK := false
	if a.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		databaseOK = a.store.Ping(ctx) == nil
		cancel()
	}
	capabilitiesReady := a.capability != nil && a.capability.Ready()
	httpx.WriteJSON(w, http.StatusOK, struct {
		Version           string `json:"version"`
		Commit            string `json:"commit"`
		BuildTime         string `json:"build_time"`
		ModelMode         string `json:"model_mode"`
		ModelName         string `json:"model_name"`
		PublicBaseURL     string `json:"public_base_url"`
		DatabaseOK        bool   `json:"database_ok"`
		CapabilitiesReady bool   `json:"capabilities_ready"`
		Quota             struct {
			DailyCalls       int  `json:"daily_calls"`
			DailyInputChars  int  `json:"daily_input_chars"`
			Concurrency      int  `json:"concurrency"`
			GlobalDailyCalls int  `json:"global_daily_calls"`
			KillSwitch       bool `json:"kill_switch"`
		} `json:"quota"`
		RateLimits struct {
			AIPerHour    int `json:"ai_per_hour"`
			AIBurst      int `json:"ai_burst"`
			APIPerMinute int `json:"api_per_minute"`
		} `json:"rate_limits"`
	}{
		Version: a.version, Commit: a.commit, BuildTime: a.buildTime,
		ModelMode: a.config.Model.Mode, ModelName: a.config.Model.Name,
		PublicBaseURL: a.config.PublicBaseURL, DatabaseOK: databaseOK,
		CapabilitiesReady: capabilitiesReady,
		Quota: struct {
			DailyCalls       int  `json:"daily_calls"`
			DailyInputChars  int  `json:"daily_input_chars"`
			Concurrency      int  `json:"concurrency"`
			GlobalDailyCalls int  `json:"global_daily_calls"`
			KillSwitch       bool `json:"kill_switch"`
		}{
			DailyCalls:       a.config.Model.DailyCallsPerUser,
			DailyInputChars:  a.config.Model.DailyInputCharsPerUser,
			Concurrency:      a.config.Model.ConcurrencyPerUser,
			GlobalDailyCalls: a.config.Model.GlobalDailyCalls,
			KillSwitch:       a.config.Model.KillSwitch,
		},
		RateLimits: struct {
			AIPerHour    int `json:"ai_per_hour"`
			AIBurst      int `json:"ai_burst"`
			APIPerMinute int `json:"api_per_minute"`
		}{AIPerHour: a.rateAILimit, AIBurst: 5, APIPerMinute: a.rateAPILimit},
	})
}

// modelCheck performs a non-completion connection check against the configured
// provider. Fixture mode performs no network call.
func (a *App) modelCheck(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r.Context())
	if !a.allowUserLimit(w, r, principal.User.ID, a.modelCheckLimiter, 5,
		"model_check_rate_limited", "too many model checks") {
		return
	}
	mode := a.config.Model.Mode
	result := CheckModelConnection(r.Context(), a.config)
	if result.Status != "fixture" && result.Status != "ok" && result.Status != "unverified_model" {
		a.logger.Warn("model_check_failed",
			"request_id", httpx.RequestIDFromContext(r.Context()), "status", result.Status)
	}
	httpx.WriteJSON(w, http.StatusOK, struct {
		Status    string `json:"status"`
		Mode      string `json:"mode"`
		ModelName string `json:"model_name"`
		Detail    string `json:"detail"`
	}{Status: result.Status, Mode: mode, ModelName: a.config.Model.Name, Detail: result.Detail})
}

// streamEvents is an authenticated refresh signal. It carries no state.
func (a *App) streamEvents(w http.ResponseWriter, r *http.Request) {
	if a.stream == nil {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "service unavailable"))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeInternal, "streaming is unavailable"))
		return
	}
	principal := principalFrom(r.Context())
	channel, unsubscribe := a.stream.Subscribe(principal.User.ID)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 5000\nevent: ready\ndata: {\"ok\":true}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	maxLifetime := time.NewTimer(4 * time.Minute)
	defer maxLifetime.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-maxLifetime.C:
			_, _ = fmt.Fprint(w, "event: reconnect\ndata: {}\n\n")
			flusher.Flush()
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-channel:
			_, _ = fmt.Fprint(w, "event: refresh\ndata: {\"reason\":\"change\"}\n\n")
			flusher.Flush()
		}
	}
}

// notifyObjectMembers signals every current member to re-fetch. The signal
// itself carries no data and every re-fetch re-authorizes.
func (a *App) notifyObjectMembers(ctx context.Context, objectID uuid.UUID) {
	if a.stream == nil || a.objects == nil {
		return
	}
	members, err := a.objects.Members(ctx, objectID)
	if err != nil {
		return
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		parsed, err := uuid.Parse(member.UserID)
		if err != nil {
			continue
		}
		ids = append(ids, parsed)
	}
	a.stream.NotifyMany(ids)
}

// notifyUser signals one user after a social or publication change.
func (a *App) notifyUser(userID uuid.UUID) {
	if a.stream != nil {
		a.stream.Notify(userID)
	}
}
