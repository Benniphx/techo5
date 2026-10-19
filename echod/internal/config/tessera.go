package config

// Tessera is whether the device offers itself to the Tessera screen add-on as a screen
// (feature/tessera). Off unless somebody turns it on: on, the device carries a sub-device of its
// own with the entities and the action the add-on looks for, and the add-on may send it layouts.
type Tessera struct {
	On bool `json:"on,omitempty"`
}

type TesseraWriter struct{ st *Store }

// On is taken back when it could not be saved, since a change to it restarts the daemon: one that did
// not hold would come back the other way after the restart.
func (w TesseraWriter) On(v bool) error {
	return w.st.UpdateOrUndo(func(c *Config) { c.Tessera.On = v })
}
