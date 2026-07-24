package collector

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jdpx/ngbs-icon-exporter/ngbs"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fakePoller returns a fixed snapshot (or error) without any network.
type fakePoller struct {
	data *ngbs.Data
	err  error
}

func (f fakePoller) Poll(context.Context) (*ngbs.Data, error) { return f.data, f.err }

func sample() *ngbs.Data {
	return &ngbs.Data{
		SysID: "500225070340", Ver: "20251125121953", HC: 1, CE: 0, On: 1,
		WTemp: 24.3, ETemp: 20.6,
		Info: ngbs.Info{Firmware: 1079, Uptime: 613, Netl: map[string]string{"MAC": "aa:bb"}},
		DP: map[string]ngbs.Room{
			"1.1": {On: 1, Live: 1, Temp: 23.2, RH: 56.9, Dew: 14.1, HC: 1, Name: "Nappali "},
			"1.4": {On: 0, Name: "Room 1.4"},
		},
	}
}

func TestCollectSkipsInactive(t *testing.T) {
	c := New(fakePoller{data: sample()}, time.Second, false, nil)

	if got := testutil.CollectAndCount(c, "ngbs_icon_room_temperature_celsius"); got != 1 {
		t.Errorf("room temperature series = %d, want 1 (inactive channel skipped)", got)
	}

	expected := `
# HELP ngbs_icon_room_temperature_celsius Measured room temperature (°C).
# TYPE ngbs_icon_room_temperature_celsius gauge
ngbs_icon_room_temperature_celsius{id="1.1",name="Nappali"} 23.2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ngbs_icon_room_temperature_celsius"); err != nil {
		t.Error(err)
	}
}

func TestCollectIncludeInactive(t *testing.T) {
	c := New(fakePoller{data: sample()}, time.Second, true, nil)
	if got := testutil.CollectAndCount(c, "ngbs_icon_room_installed"); got != 2 {
		t.Errorf("room installed series = %d, want 2 (inactive included)", got)
	}
}

func TestCollectUpZeroOnError(t *testing.T) {
	c := New(fakePoller{err: context.DeadlineExceeded}, time.Second, false, nil)
	expected := "# HELP ngbs_icon_up 1 if the last controller scrape succeeded, 0 otherwise.\n# TYPE ngbs_icon_up gauge\nngbs_icon_up 0\n"
	if err := testutil.CollectAndCompare(c, strings.NewReader(expected), "ngbs_icon_up"); err != nil {
		t.Error(err)
	}
}
