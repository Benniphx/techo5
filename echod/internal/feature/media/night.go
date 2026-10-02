package media

import (
	"context"
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Night volume: as quiet hours start, a device left loud from the afternoon's music is turned down to
// a level somebody chose, so the first answer at bedtime is not a shout; as they end, it goes back to
// where it was.
//
// It turns the volume down once, as the hours start (or as the limit is set inside them), and never
// again that night: somebody who turns it up at two in the morning wanted it up. And it puts the level
// back only if nobody has chosen another since, so a level picked overnight is not undone by the
// morning. The level it came down from is saved, so a restart in the night still puts it back.
//
// Alarms and timers ring at their own volume (config.Alarms.Ring), which starts from the daytime level
// rather than this one.

// nightCheck is how often the hours are looked at: a minute late at worst is fine for a volume.
const nightCheck = 30 * time.Second

func newNight() *esphome.Number {
	return &esphome.Number{
		Base: esphome.Base{
			ObjectID: "night_volume",
			Name:     "Night volume",
			Icon:     "mdi:volume-low",
			Category: esphome.CategoryConfig,
		},
		Min: 0, Max: VolumeSteps, Step: 1,
		Mode: esphome.NumberSlider,
	}
}

// turnDown is the level as quiet hours start, and the daytime level to remember: the limit when the
// volume is over it, else unchanged. Nothing changes without a limit, or when it was already turned
// down.
func turnDown(limit, step, day int) (to, keep int) {
	if limit <= 0 || day > 0 || step <= limit {
		return step, day
	}
	return limit, step
}

// turnUp is the level as quiet hours end: back to where it was turned down from, unless somebody set
// another level since. Either way nothing is remembered after.
func turnUp(limit, step, day int) (to, keep int) {
	if day > 0 && step == limit {
		return day, 0
	}
	return step, 0
}

// Run follows quiet hours for the night volume until ctx ends.
func (p *Player) Run(ctx context.Context) error {
	quiet := config.Get().Speaker.Quiet(time.Now())
	// Turned down before a restart that outlasted the night: the morning's turn up was missed.
	if !quiet {
		p.nightEnd()
	}
	t := time.NewTicker(nightCheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		now := config.Get().Speaker.Quiet(time.Now())
		switch {
		case now && !quiet:
			p.nightStart()
		case !now && quiet:
			p.nightEnd()
		}
		quiet = now
	}
}

// nightStart turns the volume down to the night volume, if it is over it.
func (p *Player) nightStart() {
	c := config.Get().Speaker
	to, keep := turnDown(c.NightVolume, p.Volume(), c.DayVolume)
	if to == p.Volume() {
		return
	}
	if err := config.Set().Speaker().DayVolume(keep); err != nil {
		slog.Error("saving the daytime volume failed", "err", err)
		return
	}
	p.setQuietly(to)
	slog.Info("night volume: turned down", "from", keep, "to", to)
}

// nightEnd puts the volume back where night volume found it.
func (p *Player) nightEnd() {
	c := config.Get().Speaker
	if c.DayVolume == 0 {
		return
	}
	to, _ := turnUp(c.NightVolume, p.Volume(), c.DayVolume)
	if err := config.Set().Speaker().DayVolume(0); err != nil {
		slog.Error("saving the daytime volume failed", "err", err)
	}
	if to == p.Volume() {
		slog.Info("night volume: left as it was set overnight", "step", to)
		return
	}
	p.setQuietly(to)
	slog.Info("night volume: turned back up", "to", to)
}

// setQuietly applies and saves a level without the arc: nobody turned it, and at night a ring lighting
// up in a dark room is the opposite of the point.
func (p *Player) setQuietly(step int) {
	applied := p.apply(step, false)
	if err := config.Set().Speaker().Volume(applied); err != nil {
		slog.Error("saving volume failed", "err", err)
	}
}

// SetNightVolume chooses the night volume, 0 for none, as Home Assistant and the setup page do. Chosen
// inside quiet hours, it applies at once.
func (p *Player) SetNightVolume(n int) {
	n = max(0, min(n, VolumeSteps))
	if err := config.Set().Speaker().NightVolume(n); err != nil {
		slog.Error("saving a setting failed", "setting", p.night.ObjectID, "err", err)
		return
	}
	p.night.Set(float32(n))
	slog.Info("setting changed", "setting", p.night.ObjectID, "using", n)
	if config.Get().Speaker.Quiet(time.Now()) {
		p.nightStart()
	}
}

// NightVolume is the night volume, 0 for none.
func (p *Player) NightVolume() int { return config.Get().Speaker.NightVolume }
