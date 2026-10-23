//go:build dot && donut

package all

// boardChoices is empty: the Echo Dot 3 plays only its speaker so far (hardware/speaker/paths_donut.go).
// boardAbsent is the update channel: nothing is published for it (update/releases_donut.go).
var (
	boardChoices []string
	boardAbsent  = []string{"update_channel"}
)
