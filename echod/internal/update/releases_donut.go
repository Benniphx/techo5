//go:build dot && donut

package update

// releases is empty: nothing is published for the Echo Dot 3, so it checks no channel at all rather
// than reading the Dot 2's and finding nothing it can install (Serves).
const releases = ""
