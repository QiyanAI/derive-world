// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"errors"

	"github.com/QiyanAI/derive-world/internal/config"
	"github.com/QiyanAI/derive-world/internal/model"
)

// ModelCheckResult is the single non-secret status vocabulary shared by the
// authenticated settings endpoint and the local launcher CLI. A successful
// models listing is "unverified_model": it does not prove the configured model
// name is callable and never implies an inference test.
type ModelCheckResult struct {
	Status string
	Detail string
}

// CheckModelConnection performs the non-completion provider check. It never
// sends a prompt and never returns the API key or provider response body.
func CheckModelConnection(ctx context.Context, cfg *config.Config) ModelCheckResult {
	if cfg.Model.Mode == "fixture" {
		return ModelCheckResult{
			Status: "fixture",
			Detail: "fixture mode is deterministic; no provider was contacted",
		}
	}
	err := model.CheckConnection(ctx, model.AdapterOptions{
		Mode: cfg.Model.Mode, BaseURL: cfg.Model.BaseURL,
		APIKey: cfg.Model.APIKey, ModelName: cfg.Model.Name,
		Timeout: cfg.Model.Timeout,
	})
	switch {
	case err == nil:
		return ModelCheckResult{
			Status: "unverified_model",
			Detail: "the models endpoint answered; model callability is not proven",
		}
	case errors.Is(err, model.ErrCheckUnauthorized):
		return ModelCheckResult{
			Status: "unauthorized",
			Detail: "the provider rejected the configured credentials",
		}
	case errors.Is(err, model.ErrCheckRateLimited):
		return ModelCheckResult{
			Status: "rate_limited",
			Detail: "the provider is rate limiting model checks",
		}
	case errors.Is(err, model.ErrCheckUnavailable):
		return ModelCheckResult{
			Status: "unavailable",
			Detail: "the provider is temporarily unavailable",
		}
	case errors.Is(err, model.ErrCheckTLS):
		return ModelCheckResult{
			Status: "tls_failure",
			Detail: "TLS verification failed",
		}
	case errors.Is(err, model.ErrCheckInvalidURL):
		return ModelCheckResult{
			Status: "invalid_url",
			Detail: "the configured model URL is invalid",
		}
	case errors.Is(err, model.ErrCheckUnreachable):
		return ModelCheckResult{
			Status: "unreachable",
			Detail: "the provider endpoint could not be reached",
		}
	default:
		return ModelCheckResult{
			Status: "error",
			Detail: "the provider returned an unexpected response",
		}
	}
}
