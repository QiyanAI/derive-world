// SPDX-License-Identifier: Apache-2.0
package app

import (
	"net/http"

	"github.com/QiyanAI/derive-world/internal/httpx"
	"github.com/QiyanAI/derive-world/internal/worldcap"
)

// worldCapabilities exposes the server-owned capability catalog that drives
// the product's palette and generated forms. Registering a new module makes it
// available to every world without changing the world protocol or this route.
func (a *App) worldCapabilities(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, struct {
		Items []worldcap.Descriptor `json:"items"`
	}{Items: worldcap.Default().Catalog()})
}
