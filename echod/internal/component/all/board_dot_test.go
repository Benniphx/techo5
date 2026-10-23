//go:build dot && !donut

package all

// boardChoices is what the Dot 2 and the Echo 2 offer that the Echo Dot 3 does not yet: the audio output
// (both play their jacks). boardAbsent is nothing: both have release channels.
var (
	boardChoices = []string{"audio_output"}
	boardAbsent  []string
)
