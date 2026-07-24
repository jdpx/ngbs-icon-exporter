// Package collector turns an NGBS iCON datapoll snapshot into Prometheus
// metrics. It scrapes the controller on each collection (scrape-on-collect), so
// the poll cadence is simply your Prometheus scrape interval.
package collector

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jdpx/ngbs-icon-exporter/ngbs"
	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "ngbs_icon"

// poller is the subset of *ngbs.Client the collector needs (aids testing).
type poller interface {
	Poll(ctx context.Context) (*ngbs.Data, error)
}

// Collector implements prometheus.Collector for one NGBS iCON controller.
type Collector struct {
	client          poller
	timeout         time.Duration
	includeInactive bool
	logger          *slog.Logger

	// exporter self-metrics
	up            *prometheus.Desc
	scrapeSeconds *prometheus.Desc
	info          *prometheus.Desc

	// system metrics
	sys map[string]*prometheus.Desc
	// per-room metrics
	room map[string]*prometheus.Desc
}

// New builds a Collector. If includeInactive is false, thermostat channels that
// are not installed (ON == 0) are omitted so their placeholder zero readings do
// not pollute dashboards.
func New(client poller, timeout time.Duration, includeInactive bool, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	sysLabels := []string{}
	roomLabels := []string{"id", "name"}

	sysG := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, sysLabels, nil)
	}
	roomG := func(name, help string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "room", name), help, roomLabels, nil)
	}

	return &Collector{
		client:          client,
		timeout:         timeout,
		includeInactive: includeInactive,
		logger:          logger,

		up:            prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "up"), "1 if the last controller scrape succeeded, 0 otherwise.", nil, nil),
		scrapeSeconds: prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"), "Duration of the controller scrape.", nil, nil),
		info:          prometheus.NewDesc(prometheus.BuildFQName(namespace, "", "info"), "Controller information; constant 1 with metadata labels.", []string{"sysid", "version", "firmware", "mac"}, nil),

		sys: map[string]*prometheus.Desc{
			"on":               sysG("system_on", "System powered on (1) or off (0)."),
			"cooling":          sysG("cooling_mode", "System H/C mode: 1 = cooling, 0 = heating."),
			"eco":              sysG("eco_mode", "System C/E mode: 1 = economy, 0 = comfort."),
			"water_temp":       sysG("water_temperature_celsius", "Water/flow temperature (°C)."),
			"external_temp":    sysG("external_temperature_celsius", "External/outdoor temperature (°C)."),
			"pump":             sysG("pump_active", "Circulation pump running (1) or idle (0)."),
			"error":            sysG("error", "Controller error flag."),
			"overheat":         sysG("overheat", "Overheat protection tripped."),
			"water_frost":      sysG("water_frost", "Water frost protection tripped."),
			"setpoint_heating": sysG("setpoint_heating_celsius", "System heating setpoint (°C)."),
			"setpoint_cooling": sysG("setpoint_cooling_celsius", "System cooling setpoint (°C)."),
			"setpoint_eco_h":   sysG("setpoint_eco_heating_celsius", "System eco heating setpoint (°C)."),
			"setpoint_eco_c":   sysG("setpoint_eco_cooling_celsius", "System eco cooling setpoint (°C)."),
			"uptime":           sysG("uptime_seconds", "Controller uptime (seconds)."),
		},
		room: map[string]*prometheus.Desc{
			"installed":        roomG("installed", "Thermostat channel installed/active (1) or not (0)."),
			"live":             roomG("live", "Thermostat sensor currently reporting (1) or not (0)."),
			"temperature":      roomG("temperature_celsius", "Measured room temperature (°C)."),
			"humidity":         roomG("humidity_percent", "Measured relative humidity (%)."),
			"dew_point":        roomG("dew_point_celsius", "Calculated dew point (°C)."),
			"setpoint_heating": roomG("setpoint_heating_celsius", "Room heating setpoint (°C)."),
			"setpoint_cooling": roomG("setpoint_cooling_celsius", "Room cooling setpoint (°C)."),
			"setpoint_eco_h":   roomG("setpoint_eco_heating_celsius", "Room eco heating setpoint (°C)."),
			"setpoint_eco_c":   roomG("setpoint_eco_cooling_celsius", "Room eco cooling setpoint (°C)."),
			"limit":            roomG("limit_celsius", "Manual +/- adjustment range (°C)."),
			"cooling":          roomG("cooling_mode", "Room H/C mode: 1 = cooling, 0 = heating."),
			"eco":              roomG("eco_mode", "Room C/E mode: 1 = economy, 0 = comfort."),
			"output":           roomG("output_active", "Actuator output active — room calling for heat/cool (1)."),
			"pump":             roomG("water_pump", "Room water/circulation pump demand."),
			"valve":            roomG("mixing_valve", "Room mixing valve demand."),
			"lock":             roomG("lock", "Thermostat locked (1)."),
			"condensation":     roomG("condensation", "Condensation / dew-point warning (1)."),
			"digital_input":    roomG("digital_input", "Digital input active (1)."),
			"frost":            roomG("frost", "Frost alarm (1)."),
			"reg_b_heating":    roomG("reg_b_heating", "Reg-B heating enabled (1)."),
			"reg_b_cooling":    roomG("reg_b_cooling", "Reg-B cooling enabled (1)."),
		},
	}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.scrapeSeconds
	ch <- c.info
	for _, d := range c.sys {
		ch <- d
	}
	for _, d := range c.room {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	data, err := c.client.Poll(ctx)
	ch <- prometheus.MustNewConstMetric(c.scrapeSeconds, prometheus.GaugeValue, time.Since(start).Seconds())
	if err != nil {
		c.logger.Error("scrape failed", "err", err)
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	ch <- prometheus.MustNewConstMetric(c.info, prometheus.GaugeValue, 1,
		data.SysID, data.Ver, strconv.Itoa(data.Info.Firmware), data.Info.Netl["MAC"])

	g := func(desc *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v)
	}
	g(c.sys["on"], b(data.On))
	g(c.sys["cooling"], b(data.HC))
	g(c.sys["eco"], b(data.CE))
	g(c.sys["water_temp"], data.WTemp)
	g(c.sys["external_temp"], data.ETemp)
	g(c.sys["pump"], b(data.Pump))
	g(c.sys["error"], b(data.Err))
	g(c.sys["overheat"], b(data.Overheat))
	g(c.sys["water_frost"], b(data.WFrost))
	g(c.sys["setpoint_heating"], data.XAH)
	g(c.sys["setpoint_cooling"], data.XAC)
	g(c.sys["setpoint_eco_h"], data.ECOH)
	g(c.sys["setpoint_eco_c"], data.ECOC)
	g(c.sys["uptime"], float64(data.Info.Uptime))

	for id, r := range data.DP {
		if r.On == 0 && !c.includeInactive {
			continue
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = id
		}
		rg := func(key string, v float64) {
			ch <- prometheus.MustNewConstMetric(c.room[key], prometheus.GaugeValue, v, id, name)
		}
		rg("installed", b(r.On))
		rg("live", b(r.Live))
		rg("temperature", r.Temp)
		rg("humidity", r.RH)
		rg("dew_point", r.Dew)
		rg("setpoint_heating", r.XAH)
		rg("setpoint_cooling", r.XAC)
		rg("setpoint_eco_h", r.ECOH)
		rg("setpoint_eco_c", r.ECOC)
		rg("limit", r.Lim)
		rg("cooling", b(r.HC))
		rg("eco", b(r.CE))
		rg("output", b(r.Out))
		rg("pump", b(r.WP))
		rg("valve", b(r.MV))
		rg("lock", b(r.PL))
		rg("condensation", b(r.DWP))
		rg("digital_input", b(r.DI))
		rg("frost", b(r.Frost))
		rg("reg_b_heating", b(r.DXH))
		rg("reg_b_cooling", b(r.DXC))
	}
}

// b maps an integer flag (0/non-zero) to a Prometheus boolean gauge value.
func b(v int) float64 {
	if v != 0 {
		return 1
	}
	return 0
}
