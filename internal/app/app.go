// SPDX-License-Identifier: Apache-2.0

// Package app is the composition root: it registers routes, assembles
// middleware, and serves the reference web client. It contains no business
// state transitions.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/QiyanAI/derive-world/api/openapi"
	"github.com/QiyanAI/derive-world/internal/assets"
	"github.com/QiyanAI/derive-world/internal/audit"
	"github.com/QiyanAI/derive-world/internal/capability"
	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/derive"
	"github.com/QiyanAI/derive-world/internal/handoff"
	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/identity"
	"github.com/QiyanAI/derive-world/internal/model"
	"github.com/QiyanAI/derive-world/internal/objects"
	"github.com/QiyanAI/derive-world/internal/personalai"
	"github.com/QiyanAI/derive-world/internal/publication"
	"github.com/QiyanAI/derive-world/internal/social"
	"github.com/QiyanAI/derive-world/internal/store"
	"github.com/QiyanAI/derive-world/internal/stream"
	"github.com/QiyanAI/derive-world/internal/trust"
	"github.com/QiyanAI/derive-world/internal/world"
)

// Options assembles the application.
type Options struct {
	Config     *config.Config
	Logger     *slog.Logger
	LogOutput  io.Writer
	Store      *store.Store
	Model      model.Provider
	FixtureDir string
	Version    string
	Commit     string
	BuildTime  string
	// CapabilityExternal is a test-only network injection point for the
	// external Provider client. Production leaves it zero-valued.
	CapabilityExternal capability.ExternalClientOptions
}

// RouteSpec describes a registered API route.
type RouteSpec struct {
	Method  string
	Pattern string
}

// App is the assembled HTTP application.
type App struct {
	config    *config.Config
	logger    *slog.Logger
	store     *store.Store
	model     model.Provider
	version   string
	commit    string
	buildTime string

	webRoot string
	handler http.Handler
	routes  []RouteSpec

	audit       *audit.Writer
	idem        *store.Idempotency
	identity    *identity.Service
	objects     *objects.Service
	assets      *assets.Service
	derive      *derive.Service
	trust       *trust.Service
	handoff     *handoff.Service
	personalAI  *personalai.Service
	capability  *capability.Service
	social      *social.Service
	publication *publication.Service
	world       *world.Service
	stream      *stream.Broker
	quota       *model.Quota
	cursorKey   []byte

	authIPLimiter        *httpx.Limiter
	authAccountLimiter   *httpx.Limiter
	apiLimiter           *httpx.Limiter
	anonAPILimiter       *httpx.Limiter
	aiLimiter            *httpx.Limiter
	exportLimiter        *httpx.Limiter
	publicationLimiter   *httpx.Limiter
	socialWriteLimiter   *httpx.Limiter
	friendRequestLimiter *httpx.Limiter
	userSearchLimiter    *httpx.Limiter
	reportLimiter        *httpx.Limiter
	modelCheckLimiter    *httpx.Limiter
	assetUploadLimiter   *httpx.Limiter
	rateAuthLimit        int
	rateAuthAccountLimit int
	rateAPILimit         int
	rateAILimit          int
	rateCapabilityLimit  int
}

// New validates the web root and assembles routes and middleware.
func New(options Options) (*App, error) {
	if options.Config == nil {
		return nil, errors.New("config is required")
	}
	logger := options.Logger
	if logger == nil {
		output := options.LogOutput
		if output == nil {
			output = io.Discard
		}
		logger = httpx.NewLogger(options.Config.LogLevel, options.Config.LogFormat, output)
	}
	provider := options.Model
	if provider == nil {
		created, err := model.NewWithOptions(model.AdapterOptions{
			Mode:            options.Config.Model.Mode,
			BaseURL:         options.Config.Model.BaseURL,
			APIKey:          options.Config.Model.APIKey,
			ModelName:       options.Config.Model.Name,
			Timeout:         options.Config.Model.Timeout,
			MaxOutputTokens: options.Config.Model.MaxOutputTokens,
			FixtureDir:      options.FixtureDir,
			ChatPath:        options.Config.Model.ChatPath,
			ResponseFormat:  options.Config.Model.ResponseFormat,
			ThinkingMode:    options.Config.Model.ThinkingMode,
		})
		if err != nil {
			return nil, err
		}
		provider = created
	}
	if options.Config.WebDistDir == "" {
		return nil, errors.New("WEB_DIST_DIR is required")
	}
	webRoot, err := filepath.Abs(options.Config.WebDistDir)
	if err != nil {
		return nil, fmt.Errorf("resolve WEB_DIST_DIR: %w", err)
	}
	indexPath := filepath.Join(webRoot, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return nil, fmt.Errorf("WEB_DIST_DIR must contain index.html: %w", err)
	}

	application := &App{
		config:    options.Config,
		logger:    logger,
		store:     options.Store,
		model:     provider,
		version:   options.Version,
		commit:    options.Commit,
		buildTime: options.BuildTime,
		webRoot:   webRoot,
		cursorKey: []byte(options.Config.CursorHMACSecret),
		stream:    stream.NewBroker(),
	}
	absoluteTTL := options.Config.SessionAbsoluteTTL
	if absoluteTTL <= 0 {
		absoluteTTL = 7 * 24 * time.Hour
	}
	idleTTL := options.Config.SessionIdleTTL
	if idleTTL <= 0 {
		idleTTL = 24 * time.Hour
	}
	rateAuth := options.Config.RateLimitAuthPerMin
	if rateAuth <= 0 {
		rateAuth = 10
	}
	rateAPI := options.Config.RateLimitAPIPerMin
	if rateAPI <= 0 {
		rateAPI = 120
	}
	application.rateAuthLimit = rateAuth
	application.rateAuthAccountLimit = 5
	application.rateAPILimit = rateAPI
	rateAI := options.Config.RateLimitAIPerHour
	if rateAI <= 0 {
		rateAI = 20
	}
	application.rateAILimit = rateAI
	rateCapability := options.Config.RateLimitCapPerHour
	if rateCapability <= 0 || rateCapability > 60 {
		rateCapability = 60
	}
	application.rateCapabilityLimit = rateCapability
	apiBurst := rateAPI
	if apiBurst > 60 {
		apiBurst = 60
	}
	application.authIPLimiter = httpx.NewLimiter(rateAuth, time.Minute)
	application.authAccountLimiter = httpx.NewLimiter(5, time.Minute)
	application.apiLimiter = httpx.NewLimiterWithBurst(rateAPI, time.Minute, apiBurst)
	application.anonAPILimiter = httpx.NewLimiterWithBurst(rateAPI, time.Minute, apiBurst)
	application.aiLimiter = httpx.NewLimiterWithBurst(rateAI, time.Hour, 5)
	application.exportLimiter = httpx.NewLimiter(1, time.Hour)
	application.publicationLimiter = httpx.NewLimiterWithBurst(10, time.Hour, 3)
	application.socialWriteLimiter = httpx.NewLimiterWithBurst(60, time.Minute, 20)
	application.friendRequestLimiter = httpx.NewLimiterWithBurst(20, time.Hour, 5)
	application.userSearchLimiter = httpx.NewLimiterWithBurst(60, time.Minute, 20)
	application.reportLimiter = httpx.NewLimiterWithBurst(5, time.Hour, 2)
	application.modelCheckLimiter = httpx.NewLimiterWithBurst(5, time.Hour, 2)
	application.assetUploadLimiter = httpx.NewLimiterWithBurst(30, time.Hour, 10)
	if options.Store != nil {
		application.audit = audit.NewWriter(options.Store.Pool())
		application.idem = options.Store.Idempotency()
		// The Trust Kernel is the single authorization decision point. It is
		// constructed first and injected into objects as the exact-delegation
		// revalidator for proposal apply; this keeps package objects free of a
		// trust import (D08 §3.2).
		application.trust = trust.NewService(options.Store.Pool(), application.audit, application.idem)
		application.objects = objects.NewService(options.Store.Pool(), application.audit, application.idem, application.trust)
		assetDir := options.Config.AssetDir
		if strings.TrimSpace(assetDir) == "" {
			assetDir = "./var/assets"
		}
		absoluteAssetDir, assetDirErr := filepath.Abs(assetDir)
		if assetDirErr != nil {
			return nil, fmt.Errorf("resolve ASSET_DIR: %w", assetDirErr)
		}
		if absoluteAssetDir == webRoot || strings.HasPrefix(absoluteAssetDir, webRoot+string(os.PathSeparator)) {
			return nil, errors.New("ASSET_DIR must not be inside WEB_DIST_DIR")
		}
		assetService, assetErr := assets.New(options.Store.Pool(), application.audit, absoluteAssetDir)
		if assetErr != nil {
			return nil, assetErr
		}
		assetService.SetGenerator(assets.ProceduralGenerator{})
		assetService.SweepTemp(time.Hour)
		application.assets = assetService
		application.derive = derive.NewService(options.Store.Pool(), application.objects, application.idem)
		application.social = social.NewService(options.Store.Pool(), application.audit)
		application.publication = publication.NewService(options.Store.Pool(), application.objects, application.assets, application.audit, application.idem)
		// Event timelines use the publication-level source for public-derived
		// children instead of exposing the private parent object id.
		application.objects.SetPublicSourceResolver(application.publication.PublicSourceForChild)
		application.world = world.NewService(options.Store.Pool())
		identityService, identityErr := identity.NewService(options.Store.Pool(), application.audit, absoluteTTL, idleTTL)
		if identityErr != nil {
			return nil, identityErr
		}
		application.identity = identityService
		application.handoff = handoff.NewService(options.Store.Pool(), application.audit)
		application.quota = model.NewQuota(options.Store.Pool(), model.QuotaConfig{
			Mode:             options.Config.Model.Mode,
			DailyCalls:       options.Config.Model.DailyCallsPerUser,
			DailyInputChars:  options.Config.Model.DailyInputCharsPerUser,
			Concurrency:      options.Config.Model.ConcurrencyPerUser,
			MaxInputChars:    options.Config.Model.MaxInputChars,
			GlobalDailyCalls: options.Config.Model.GlobalDailyCalls,
			GlobalDailyChars: options.Config.Model.GlobalDailyInputChars,
			Disabled:         options.Config.Model.KillSwitch,
		})

		projector := func(ctx context.Context, source capability.ContextSource,
			actor audit.Actor, requestID string, ipHash []byte) (json.RawMessage, error) {
			projection, err := application.handoff.Build(ctx, handoff.Source{
				ObjectID: source.ObjectID, ObjectType: source.ObjectType,
				Version: source.Version, State: source.State,
				SharedContext: source.SharedContext, Permissions: source.Permissions,
			}, actor, requestID, ipHash)
			if err != nil {
				return nil, err
			}
			return json.Marshal(projection)
		}
		capabilityService, capabilityErr := capability.New(capability.Options{
			DB:               options.Store.Pool(),
			Audit:            application.audit,
			Idempotency:      application.idem,
			Objects:          application.objects,
			Trust:            application.trust,
			Projector:        projector,
			Deriver:          &deriveAdapter{service: application.derive},
			ConfigPath:       options.Config.CapabilitiesConfig,
			HMACSecret:       options.Config.CapabilityHMACSecret,
			HMACKeyID:        options.Config.CapabilityHMACKeyID,
			RateLimitPerHour: application.rateCapabilityLimit,
			Logger:           logger,
			ExternalOptions:  options.CapabilityExternal,
		})
		if capabilityErr != nil {
			return nil, capabilityErr
		}
		initCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		initErr := capabilityService.Initialize(initCtx)
		cancel()
		if initErr != nil {
			return nil, fmt.Errorf("capability initialization: %w", initErr)
		}
		application.capability = capabilityService

		personalAI, personalErr := personalai.NewService(personalai.Options{
			DB: options.Store.Pool(), Provider: provider,
			ModelName: options.Config.Model.Name,
			Handoff:   application.handoff, Trust: application.trust,
			Proposals:    application.objects,
			Capabilities: &personalAICapabilityWriter{service: capabilityService},
			Audit:        application.audit,
			Idempotency:  application.idem, Recorder: model.NewRecorder(),
			Quota:                 application.quota,
			IncludePrivateContext: options.Config.Model.IncludePrivateContext,
			Logger:                logger,
		})
		if personalErr != nil {
			return nil, personalErr
		}
		application.personalAI = personalAI
	}

	mux := http.NewServeMux()
	application.routes = []RouteSpec{
		{Method: http.MethodGet, Pattern: "/healthz"},
		{Method: http.MethodGet, Pattern: "/readyz"},
		{Method: http.MethodGet, Pattern: "/api/v1/meta"},
		{Method: http.MethodGet, Pattern: "/api/v1/openapi.yaml"},
	}
	mux.Handle("/healthz", httpx.TagRoute("/healthz", methodGuard(http.MethodGet, application.healthz)))
	mux.Handle("/readyz", httpx.TagRoute("/readyz", methodGuard(http.MethodGet, application.readyz)))
	mux.Handle("/api/v1/meta", httpx.TagRoute("/api/v1/meta", methodGuard(http.MethodGet, application.meta)))
	mux.Handle("/api/v1/openapi.yaml", httpx.TagRoute("/api/v1/openapi.yaml", methodGuard(http.MethodGet, application.openapiDocument)))
	application.registerBusinessRoutes(mux)
	mux.Handle("/", http.HandlerFunc(application.static))

	application.handler = httpx.Chain(
		mux,
		httpx.RequestID,
		httpx.SecurityHeaders,
		httpx.AccessLog(logger, options.Config.IPHashPepper, options.Config.TrustProxyHeaders),
		httpx.Recover(logger),
		httpx.CORS(options.Config.CORSAllowedOrigins),
		httpx.BodyLimitExcept(options.Config.MaxRequestBytes, isAssetUploadRequest),
	)
	return application, nil
}

// Handler returns the composed HTTP handler.
func (a *App) Handler() http.Handler {
	return a.handler
}

// Logger returns the application logger for lifecycle messages.
func (a *App) Logger() *slog.Logger {
	return a.logger
}

// Routes returns the API route table (static routes excluded).
func (a *App) Routes() []RouteSpec {
	out := make([]RouteSpec, len(a.routes))
	copy(out, a.routes)
	return out
}

func methodGuard(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method && !(method == http.MethodGet && r.Method == http.MethodHead) {
			w.Header().Set("Allow", method+", HEAD")
			httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
				"error": map[string]any{
					"code":       httpx.CodeInvalidRequest,
					"message":    "method not allowed",
					"request_id": httpx.RequestIDFromContext(r.Context()),
					"details":    map[string]any{},
				},
			})
			return
		}
		next(w, r)
	}
}

func (a *App) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func (a *App) readyz(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	if a.capability == nil || !a.capability.Ready() {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

type metaResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	ModelMode string `json:"model_mode"`
}

func (a *App) meta(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, metaResponse{
		Version:   a.version,
		Commit:    a.commit,
		BuildTime: a.buildTime,
		ModelMode: a.config.Model.Mode,
	})
}

func (a *App) openapiDocument(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapi.Spec)
}

func (a *App) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		httpx.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": map[string]any{
				"code":       httpx.CodeInvalidRequest,
				"message":    "method not allowed",
				"request_id": httpx.RequestIDFromContext(r.Context()),
				"details":    map[string]any{},
			},
		})
		return
	}

	cleanPath := path.Clean("/" + r.URL.Path)
	if cleanPath == "/" {
		a.serveIndex(w, r)
		return
	}
	fullPath := filepath.Join(a.webRoot, filepath.FromSlash(cleanPath))
	if !strings.HasPrefix(fullPath, a.webRoot+string(os.PathSeparator)) {
		httpx.WriteError(w, r, httpx.NewError(httpx.CodeNotFound, "not found"))
		return
	}
	info, err := os.Stat(fullPath)
	if err == nil && !info.IsDir() {
		if strings.HasPrefix(cleanPath, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, fullPath)
		return
	}
	a.serveIndex(w, r)
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(a.webRoot, "index.html"))
}

// isAssetUploadRequest exempts the raw media upload route from the JSON body
// limit. The asset handler applies the per-kind ceiling and reads through a
// bounded reader; every other route keeps the global JSON limit.
func isAssetUploadRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	return len(segments) == 5 && segments[0] == "api" && segments[1] == "v1" &&
		segments[2] == "objects" && segments[4] == "assets"
}
