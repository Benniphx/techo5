//go:build dot || spot

package setup

import "net/http"

// Videos are the Show's alone: on the Spot and the Dot there is no section, and a form posted for one
// is refused.

func videoSection(http.ResponseWriter, string) {}

func saveVideo(*http.Request) string { return "this device does not play videos" }
