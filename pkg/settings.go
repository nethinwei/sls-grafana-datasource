package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// settings is the data source configuration: jsonData plus decrypted secureJsonData.
type settings struct {
	Endpoint      string // data source URL, e.g. cn-hangzhou.log.aliyuncs.com
	Project       string `json:"project"`
	LogStore      string `json:"logstore"`
	Region        string `json:"region"`        // set to sign requests with AuthV4
	International bool   `json:"international"` // console on alibabacloud.com instead of aliyun.com
	// SQLTimeZone is the zone of date-time strings returned by SLS SQL (timestamp columns,
	// from_unixtime, date_format), which carry no offset. SLS uses UTC+8 by default.
	SQLTimeZone string     `json:"sqlTimeZone"`
	Logs        logsConfig `json:"logs"`
	Limits      limits     `json:"limits"`

	view            logsView // Logs compiled once per instance
	sqlLocation     *time.Location
	AccessKeyID     string
	AccessKeySecret string
}

// limits bound the memory one query result may take; 0 means the default.
type limits struct {
	// MaxWideCells bounds the cells of an SQL result turned into wide time series: distinct times ×
	// combinations of string columns × number columns.
	MaxWideCells int `json:"maxWideCells"`
	// MaxFlattenedFields bounds the distinct fields the json rule creates per result.
	MaxFlattenedFields int `json:"maxFlattenedFields"`
}

const (
	defaultMaxWideCells       = 6_000_000 // as plugin 2.x's maxPointsLimit
	defaultMaxFlattenedFields = 200
)

// logsConfig is how log rows are processed and presented (jsonData.logs).
type logsConfig struct {
	// Rules reshape log rows (see rules.go). Absent means defaultFieldRules; [] means no rules.
	Rules *[]fieldRule `json:"rules"`
	// TimeField holds the row timestamp after the rules (default "timestamp").
	TimeField string `json:"timeField"`
	// LevelField holds the severity (default "level"): level colours and the logs volume.
	LevelField string `json:"levelField"`
	Line       struct {
		// Fields open the log line in this order, e.g. host.name, host.ip.
		Fields []string `json:"fields"`
		// Rest appends the other business fields (default true).
		Rest *bool `json:"rest"`
		// Exclude keeps fields out of the line; they stay in the log details.
		Exclude []string `json:"exclude"`
	} `json:"line"`
}

func loadSettings(s backend.DataSourceInstanceSettings) (*settings, error) {
	cfg := &settings{}
	if len(s.JSONData) > 0 {
		if err := json.Unmarshal(s.JSONData, cfg); err != nil {
			return nil, fmt.Errorf("read data source settings: %w", err)
		}
	}
	cfg.Endpoint = strings.TrimSpace(s.URL)
	// The SDK defaults to plain http; AccessKey-signed requests and log data go over https unless
	// the endpoint names http explicitly.
	if cfg.Endpoint != "" && !strings.Contains(cfg.Endpoint, "://") {
		cfg.Endpoint = "https://" + cfg.Endpoint
	}
	cfg.AccessKeyID = s.DecryptedSecureJSONData["accessKeyId"]
	cfg.AccessKeySecret = s.DecryptedSecureJSONData["accessKeySecret"]

	if cfg.SQLTimeZone == "" {
		cfg.SQLTimeZone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(cfg.SQLTimeZone)
	if err != nil {
		return nil, fmt.Errorf("invalid SQL time zone %q: %w", cfg.SQLTimeZone, err)
	}
	cfg.sqlLocation = loc
	if cfg.Limits.MaxWideCells <= 0 {
		cfg.Limits.MaxWideCells = defaultMaxWideCells
	}
	if cfg.Limits.MaxFlattenedFields <= 0 {
		cfg.Limits.MaxFlattenedFields = defaultMaxFlattenedFields
	}
	cfg.view = compileLogsView(cfg.Logs)
	cfg.view.maxFlattened = cfg.Limits.MaxFlattenedFields

	switch {
	case cfg.Endpoint == "":
		return nil, errors.New("endpoint (URL) is not configured")
	case cfg.Project == "":
		return nil, errors.New("project is not configured")
	case cfg.AccessKeyID == "" || cfg.AccessKeySecret == "":
		return nil, errors.New("AccessKey is not configured")
	}
	return cfg, nil
}

// consoleDomain is the second-level domain of the SLS console for this account.
func (s *settings) consoleDomain() string {
	if s.International {
		return "alibabacloud"
	}
	return "aliyun"
}
