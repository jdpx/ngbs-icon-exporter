// Package ngbs is a small client for the local web/JSON interface exposed by
// NGBS iCON heating/cooling controllers (https://ngbs.hu).
//
// The controller ships a PHP web UI on port 80. There is no documented API, so
// this client speaks the same two form-POSTs the UI's JavaScript uses:
//
//	POST /index.php  sysid=<id>&password=<pw>&lang=en&tab=login&form=login
//	    -> {"result":"success",...}  and a PHPSESSID cookie
//	POST /index.php  tab=datapoll
//	    -> a JSON document with the whole system + per-thermostat state
//
// It is unofficial and not affiliated with or endorsed by NGBS.
package ngbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// errUnauthenticated is returned internally when a datapoll response is not the
// expected JSON (the controller serves the HTML login page when the session has
// expired), so Poll knows to log in again and retry.
var errUnauthenticated = errors.New("ngbs: session not authenticated")

// Room is the live state of a single thermostat channel (DP["<base>.<channel>"]).
type Room struct {
	On    int     `json:"ON"`    // channel installed/active
	IHC   int     `json:"IHC"`   // installed heat/cool capability
	Live  int     `json:"LIVE"`  // sensor currently reporting
	Temp  float64 `json:"TEMP"`  // measured temperature (°C)
	RH    float64 `json:"RH"`    // relative humidity (%)
	Dew   float64 `json:"DEW"`   // dew point (°C)
	Lim   float64 `json:"LIM"`   // manual +/- adjustment range (°C)
	DWP   int     `json:"DWP"`   // condensation / dew-point warning
	Frost int     `json:"FROST"` // frost alarm
	CE    int     `json:"CE"`    // 0 = comfort, 1 = economy
	HC    int     `json:"HC"`    // 0 = heating, 1 = cooling
	DI    int     `json:"DI"`    // digital input
	XAH   float64 `json:"XAH"`   // heating setpoint (°C)
	XAC   float64 `json:"XAC"`   // cooling setpoint (°C)
	ECOH  float64 `json:"ECOH"`  // eco heating setpoint (°C)
	ECOC  float64 `json:"ECOC"`  // eco cooling setpoint (°C)
	PL    int     `json:"PL"`    // panel/thermostat lock
	CEF   int     `json:"CEF"`   // comfort/eco follow enabled
	CEC   int     `json:"CEC"`   // comfort/eco control enabled
	DXH   int     `json:"DXH"`   // Reg-B heating enabled
	DXC   int     `json:"DXC"`   // Reg-B cooling enabled
	Out   int     `json:"OUT"`   // actuator output active (calling for heat/cool)
	WP    int     `json:"WP"`    // water/circulation pump demand
	MV    int     `json:"MV"`    // mixing valve demand
	TPR   int     `json:"TPR"`   // time-programme active
	Name  string  `json:"NAME"`  // user-assigned room name
}

// Info is the INFO block of a datapoll response.
type Info struct {
	Firmware int               `json:"FIRMWARE"`
	Uptime   int64             `json:"UPTIME"`
	Task     []string          `json:"TASK"`
	Netl     map[string]string `json:"NETL"`
}

// Data is a full datapoll response: system-wide state plus every thermostat.
type Data struct {
	SysID    string          `json:"SYSID"`
	Service  int             `json:"SERVICE"`
	Ver      string          `json:"VER"`
	HC       int             `json:"HC"`       // system mode: 0 = heating, 1 = cooling
	CE       int             `json:"CE"`       // system mode: 0 = comfort, 1 = economy
	On       int             `json:"ON"`       // system on
	ETemp    float64         `json:"ETEMP"`    // external/outdoor temperature (°C)
	WTemp    float64         `json:"WTEMP"`    // water/flow temperature (°C)
	Pump     int             `json:"PUMP"`     // circulation pump running
	Err      int             `json:"ERR"`      // error flag
	Overheat int             `json:"OVERHEAT"` // overheat protection tripped
	WFrost   int             `json:"WFROST"`   // water frost protection tripped
	XAH      float64         `json:"XAH"`      // system heating setpoint (°C)
	XAC      float64         `json:"XAC"`      // system cooling setpoint (°C)
	ECOH     float64         `json:"ECOH"`     // system eco heating setpoint (°C)
	ECOC     float64         `json:"ECOC"`     // system eco cooling setpoint (°C)
	Sig      int             `json:"SIG"`
	SW       int             `json:"SW"`
	TZ       string          `json:"TZ"`
	DP       map[string]Room `json:"DP"`
	Info     Info            `json:"INFO"`
}

// Client talks to a single NGBS iCON controller. It is safe for concurrent use;
// Poll serialises access and transparently re-logs-in when the session expires.
type Client struct {
	baseURL  string
	sysID    string
	password string
	http     *http.Client

	mu       sync.Mutex
	loggedIn bool
}

// New returns a Client for the controller at baseURL (e.g. "http://192.168.0.10").
// If password is empty the SysID is used as the password, matching the
// controller's factory default (the login page copies SysID into the password
// field). timeout bounds each HTTP request.
func New(baseURL, sysID, password string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("ngbs: controller URL is required")
	}
	if strings.TrimSpace(sysID) == "" {
		return nil, errors.New("ngbs: SysID is required")
	}
	if password == "" {
		password = sysID
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("ngbs: cookie jar: %w", err)
	}
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		sysID:    sysID,
		password: password,
		http:     &http.Client{Jar: jar, Timeout: timeout},
	}, nil
}

// Poll logs in if needed, fetches a datapoll snapshot and returns it. A single
// re-login is attempted if the session has expired.
func (c *Client) Poll(ctx context.Context) (*Data, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loggedIn {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		c.loggedIn = true
	}

	data, err := c.datapoll(ctx)
	if errors.Is(err, errUnauthenticated) {
		// Session expired: log in once more and retry.
		if err := c.login(ctx); err != nil {
			c.loggedIn = false
			return nil, err
		}
		c.loggedIn = true
		data, err = c.datapoll(ctx)
	}
	if err != nil {
		c.loggedIn = false
		return nil, err
	}
	return data, nil
}

func (c *Client) login(ctx context.Context) error {
	form := url.Values{
		"sysid":    {c.sysID},
		"password": {c.password},
		"lang":     {"en"},
		"tab":      {"login"},
		"form":     {"login"},
	}
	body, err := c.post(ctx, form)
	if err != nil {
		return fmt.Errorf("ngbs: login request: %w", err)
	}
	var res struct {
		Result string `json:"result"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return fmt.Errorf("ngbs: login response was not JSON (wrong URL?): %w", err)
	}
	if res.Result != "success" {
		if res.Reason != "" {
			return fmt.Errorf("ngbs: login rejected: %s", res.Reason)
		}
		return errors.New("ngbs: login rejected (check SysID/password)")
	}
	return nil
}

func (c *Client) datapoll(ctx context.Context) (*Data, error) {
	body, err := c.post(ctx, url.Values{"tab": {"datapoll"}})
	if err != nil {
		return nil, fmt.Errorf("ngbs: datapoll request: %w", err)
	}
	var data Data
	if err := json.Unmarshal(body, &data); err != nil || data.SysID == "" {
		// A session-expired controller serves the HTML login page instead of
		// JSON; treat anything that isn't a well-formed datapoll as a signal to
		// re-authenticate.
		return nil, errUnauthenticated
	}
	return &data, nil
}

func (c *Client) post(ctx context.Context, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/index.php",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return body, nil
}
